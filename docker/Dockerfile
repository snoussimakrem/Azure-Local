FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/azlocal ./cmd/azlocal

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/azlocal /usr/local/bin/azlocal
EXPOSE 4577
VOLUME ["/data"]
ENV AZLOCAL_DATA_DIR=/data
ENTRYPOINT ["azlocal"]
CMD ["start", "--port", "4577"]
