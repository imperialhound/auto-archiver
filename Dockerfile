ARG GO_VERSION=1.22

FROM golang:${GO_VERSION}-alpine AS build

ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64

WORKDIR /src

COPY ./go.mod ./go.sum ./
RUN go mod download

COPY . /src

RUN go build -o /auto-archiver

FROM gcr.io/distroless/static AS final

USER nonroot:nonroot
COPY --from=build --chown=nonroot:nonroot /auto-archiver /auto-archiver
ENTRYPOINT ["/auto-archiver"]
