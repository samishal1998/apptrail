FROM alpine:3.23

ARG APPTRAIL_VERSION=latest

# This file is standalone: the verified release installer supplies the binary.
RUN apk add --no-cache ca-certificates curl tar tzdata \
    && if [ "$APPTRAIL_VERSION" = latest ]; then release=latest/download; else release="download/$APPTRAIL_VERSION"; fi \
    && curl --fail --silent --show-error --location --retry 3 --proto '=https' --proto-redir '=https' \
       "https://github.com/samishal1998/apptrail/releases/$release/install.sh" -o /tmp/install.sh \
    && sh /tmp/install.sh --version "$APPTRAIL_VERSION" --dir /usr/local/bin \
    && rm /tmp/install.sh \
    && apk del curl \
    && addgroup -S -g 10001 apptrail \
    && adduser -S -u 10001 -G apptrail apptrail \
    && mkdir /data && chown apptrail:apptrail /data && chmod 700 /data

USER apptrail
WORKDIR /data
EXPOSE 8080
VOLUME /data
ENTRYPOINT ["apptrail"]
CMD ["-addr", "0.0.0.0:8080", "-data", "/data"]
