FROM golang:1.27.3-trixie AS build
ENV CGO_ENABLED=0
RUN go install github.com/go-delve/delve/cmd/dlv@v1.27.2
WORKDIR /src
COPY . .
RUN go build -o /peek-a-go .

FROM golang:1.27.3-trixie
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOPROXY=off GOCACHE=/gocache HOME=/tmp
COPY --from=build /go/bin/dlv /peek-a-go /usr/local/bin/
COPY _warmup/main.go /warm/
# Warm the build cache with the flags dlv uses so traces don't recompile the standard library.
RUN mkdir /gocache && cd /warm && \
    go mod init warm && go build -gcflags='all=-N -l' -o /dev/null . && \
    cd / && rm -rf /warm && chmod -R a+rX /gocache
# The container runs with a read-only root, go needs a writable cache so copy it to the /tmp tmpfs.
ENV GOCACHE=/tmp/gocache
USER nobody
ENTRYPOINT ["sh", "-c", "cp -r /gocache \"$GOCACHE\" && exec peek-a-go \"$@\"", "peek-a-go"]
