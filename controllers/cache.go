package controllers

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/labstack/echo/v4"
)

var (
	acceptHeader      = textproto.CanonicalMIMEHeaderKey("Accept")
	cacheHTTP         *expirable.LRU[string, cacheableResponse]
	cacheableStatuses = map[int]bool{
		http.StatusOK:        true,
		http.StatusNoContent: true,
	}
	cacheableEndpoints = []*regexp.Regexp{
		regexp.MustCompile(`^GET /v2/$`),
		regexp.MustCompile(`^HEAD /v2/$`),

		regexp.MustCompile(`^GET /v2/_catalog$`),
		regexp.MustCompile(`^GET /v2/_catalog\?n=\d*$`),

		regexp.MustCompile(`^GET /v2/.*/tags/list$`),
		regexp.MustCompile(`^GET /v2/.*/tags/list\?n=\d*$`),

		regexp.MustCompile(`^HEAD /v2/.*/manifests/.*$`),

		regexp.MustCompile(`^HEAD /v2/.*/blobs/sha256:[a-zA-Z0-9-_]*$`),
	}
)

type cacheableResponse struct {
	ContentLength int64
	StatusCode    int
	Header        http.Header
	Body          []byte
}

func (c *cacheableResponse) Response() *http.Response {
	return &http.Response{
		ContentLength: c.ContentLength,
		StatusCode:    c.StatusCode,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        c.Header.Clone(),
		Body:          io.NopCloser(bytes.NewReader(c.Body)),
	}
}

type recorder struct {
	http.ResponseWriter
	body bytes.Buffer
}

func (r *recorder) Write(b []byte) (int, error) {
	i, err := r.ResponseWriter.Write(b)
	if err != nil {
		return i, err
	}
	return r.body.Write(b[:i])
}

func isEnpointCacheable(c echo.Context) bool {
	endpoint := c.Request().Method + " " + c.Request().URL.String()
	for _, re := range cacheableEndpoints {
		if re.MatchString(endpoint) {
			return true
		}
	}
	return false
}

func returnCached(c echo.Context, cachekey string) bool {
	if cached, ok := cacheHTTP.Get(cachekey); ok {
		resp := cached.Response() //nolint:bodyclose // it's io.NopCloser
		resp.Header.Set("X-Cache", "HIT")
		for k := range resp.Header {
			c.Response().Header().Set(k, resp.Header.Get(k))
		}
		c.Response().WriteHeader(resp.StatusCode)
		c.Response().Write(cached.Body) //nolint:errcheck // ignore error
		return true
	}
	return false
}

func cacheResponse(c echo.Context, rec *recorder, cachekey string) bool {
	var status int
	if s, ok := c.Get("resp.status").(int); ok {
		status = s
	} else {
		status = c.Response().Status
	}
	if !cacheableStatuses[status] {
		return false
	}
	headers := c.Response().Header().Clone()
	headers.Del("Date")
	resp := cacheableResponse{
		ContentLength: int64(rec.body.Len()),
		StatusCode:    c.Response().Status,
		Header:        headers,
		Body:          rec.body.Bytes(),
	}
	cacheHTTP.Add(cachekey, resp)
	return true
}

func getCacheKey(c echo.Context) string {
	hasher := sha256.New()
	hasher.Write([]byte(c.Request().Method))
	hasher.Write([]byte(c.Request().URL.String()))
	acceptValues := c.Request().Header[acceptHeader]
	if len(acceptValues) > 0 {
		sort.Strings(acceptValues)
		hasher.Write([]byte(strings.Join(acceptValues, ",")))
	}

	return fmt.Sprintf("%x", hasher.Sum(nil))
}

func cache() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cachekey := getCacheKey(c)
			c.Set("cache.key", cachekey)
			cacheable := isEnpointCacheable(c)
			log := ctxLog(c)
			if !cacheable {
				log.Debug().Msg("not cacheable")
				return next(c)
			}

			if returnCached(c, cachekey) {
				return nil
			}

			rec := &recorder{c.Response().Writer, bytes.Buffer{}}
			c.Response().Writer = rec
			c.Response().Header().Set("X-Cache", "MISS")
			log.Debug().Msg("cache miss")
			if err := next(c); err != nil {
				return err
			}

			if cacheResponse(c, rec, cachekey) {
				log.Debug().Msg("caching")
			} else {
				log.Debug().Msg("not cacheable status")
			}
			return nil
		}
	}
}
