FROM scratch

# The GitHub API is HTTPS-only, so the image needs a trust store.
COPY --from=alpine:latest /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY bot /bot

EXPOSE 3000
EXPOSE 3100

ENTRYPOINT ["/bot"]
