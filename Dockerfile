FROM registry.gitlab.com/etke.cc/base/build AS builder

WORKDIR /app
COPY . .
RUN just build

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/drp /bin/docker-registry-proxy
USER app
ENTRYPOINT ["/bin/docker-registry-proxy"]
