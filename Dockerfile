FROM golang:1.17.6 as builder

WORKDIR /tmp/transmuxer

COPY . .

ARG BUILDER
ARG VERSION

ENV TRANSMUXER_BUILDER=${BUILDER}
ENV TRANSMUXER_VERSION=${VERSION}

RUN apt-get update && apt-get install make git gcc -y && \
    make build_deps && \
    make

FROM alfg/ffmpeg:latest

WORKDIR /app

COPY --from=builder /tmp/transmuxer/bin/transmuxer .

CMD ["/app/transmuxer"]
