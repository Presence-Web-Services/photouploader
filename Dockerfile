FROM golang:alpine as go-builder

RUN mkdir /photouploader
WORKDIR /photouploader
COPY go.mod .
COPY go.sum .

RUN go mod download
COPY . .

RUN go build -o /go/bin/photouploader

FROM alpine

WORKDIR /go/bin
COPY --from=go-builder /go/bin/photouploader .
COPY asset-bucket-sa-key.json .
EXPOSE 80
ENTRYPOINT ["./photouploader"]
