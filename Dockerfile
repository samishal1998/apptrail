FROM node:24-alpine AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
COPY apptrail_working_pack/ /src/apptrail_working_pack/
RUN npm run build

FROM golang:1.26-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY --from=frontend /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /apptrail .

FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 apptrail \
    && adduser -S -u 10001 -G apptrail apptrail \
    && mkdir /data && chown apptrail:apptrail /data
COPY --from=backend /apptrail /usr/local/bin/apptrail
USER apptrail
EXPOSE 8080
VOLUME /data
ENTRYPOINT ["apptrail"]
CMD ["-addr", "0.0.0.0:8080", "-data", "/data"]
