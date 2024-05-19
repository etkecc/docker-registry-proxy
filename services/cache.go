package services

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
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/labstack/echo/v4"

	"gitlab.com/etke.cc/docker-registry-proxy/metrics"
	"gitlab.com/etke.cc/docker-registry-proxy/utils"
)

var (
	// acceptHeader is the canonicalized "Accept" header key.
	acceptHeader = textproto.CanonicalMIMEHeaderKey("Accept")
	// cacheableStatuses is a map of HTTP status codes that are cacheable.
	cacheableStatuses = map[int]bool{
		http.StatusOK:        true,
		http.StatusNoContent: true,
	}
	// cacheableEndpoints is a list of regular expressions that match cacheable endpoints.
	cacheableEndpoints = []*regexp.Regexp{
		regexp.MustCompile(`^GET /v2/$`),
		regexp.MustCompile(`^HEAD /v2/$`),

		regexp.MustCompile(`^GET /v2/_catalog$`),
		regexp.MustCompile(`^GET /v2/_catalog\?n=\d*$`),

		regexp.MustCompile(`^GET /v2/.*/tags/list$`),
		regexp.MustCompile(`^GET /v2/.*/tags/list\?n=\d*$`),

		regexp.MustCompile(`^HEAD /v2/.*/manifests/.*$`),
	}
)

// Cache is a middleware that caches responses according to the Docker Registry API v2 specification, cacheable endpoints and status codes.
type Cache struct {
	backend *expirable.LRU[string, cached]
}

// NewCache returns a new Cache instance.
func NewCache(ttl, size int) *Cache {
	return &Cache{
		backend: expirable.NewLRU[string, cached](size, nil, time.Duration(ttl)*time.Minute),
	}
}

// Middleware returns a new echo.MiddlewareFunc that caches responses according to the Docker Registry API v2 specification, cacheable endpoints and status codes.
func (cache *Cache) Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			go metrics.Request(c.Request().Method, c.Request().URL.Path)
			cachekey := cache.key(c)
			cacheable := isCacheable(c)
			log := utils.NewLog(c)

			if !cacheable {
				log.Debug().Msg("not cacheable")
				return next(c)
			}

			if cache.returnCached(c, cachekey) {
				log.Info().Msg("cache hit")
				go metrics.Cache(true)
				return nil
			}

			rec, err := cache.record(c, next)
			if err != nil {
				return err
			}

			if cache.store(c, rec, cachekey) {
				log.Debug().Msg("cache miss")
				go metrics.Cache(false)
				return nil
			}

			return nil
		}
	}
}

func (cache *Cache) returnCached(c echo.Context, cachekey string) bool {
	if v, ok := cache.backend.Get(cachekey); ok {
		resp := v.Response() //nolint:bodyclose // it's io.NopCloser
		resp.Header.Set("X-Cache", "HIT")
		for k := range resp.Header {
			c.Response().Header().Set(k, resp.Header.Get(k))
		}
		c.Response().WriteHeader(resp.StatusCode)
		c.Response().Write(v.Body) //nolint:errcheck // ignore error
		return true
	}
	return false
}

func (cache *Cache) record(c echo.Context, next echo.HandlerFunc) (*recorder, error) {
	rec := &recorder{c.Response().Writer, bytes.Buffer{}}
	c.Response().Writer = rec
	c.Response().Header().Set("X-Cache", "MISS")
	err := next(c)
	return rec, err
}

func (cache *Cache) store(c echo.Context, rec *recorder, cachekey string) bool {
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
	resp := cached{
		ContentLength: int64(rec.body.Len()),
		StatusCode:    c.Response().Status,
		Header:        headers,
		Body:          rec.body.Bytes(),
	}
	cache.backend.Add(cachekey, resp)
	return true
}

func (cache *Cache) key(c echo.Context) string {
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

type cached struct {
	ContentLength int64
	StatusCode    int
	Header        http.Header
	Body          []byte
}

func (c *cached) Response() *http.Response {
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

func isCacheable(c echo.Context) bool {
	endpoint := c.Request().Method + " " + c.Request().URL.String()
	for _, re := range cacheableEndpoints {
		if re.MatchString(endpoint) {
			return true
		}
	}
	return false
}
