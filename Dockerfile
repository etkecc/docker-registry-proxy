FROM registry.gitlab.com/etke.cc/base/build AS builder

WORKDIR /app
COPY . .
RUN just build

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/iap /bin/iap
USER app
ENTRYPOINT ["/bin/iap"]
