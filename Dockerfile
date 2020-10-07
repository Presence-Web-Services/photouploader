FROM golang:alpine as go-builder

RUN mkdir /photouploader
WORKDIR /photouploader
COPY go.mod .
COPY go.sum .

RUN go mod download
COPY . .

RUN go build -o /go/bin/photouploader

FROM alpine
ARG VERSION=3.3.1

RUN apk --update add \
	autoconf \
	automake \
	build-base \
	libtool \
	nasm \
	pkgconf \
	tar \
	pngquant \
	libwebp \
	libwebp-tools \
	imagemagick \
  bash \
  git \
  npm \
  hugo \
  openssh
RUN apk --update --repository http://dl-3.alpinelinux.org/alpine/edge/testing/ add \
	netpbm

WORKDIR /src
ADD https://github.com/mozilla/mozjpeg/archive/v${VERSION}.tar.gz ./
RUN tar -xzf v${VERSION}.tar.gz
RUN cd /src/mozjpeg-${VERSION} && \
	autoreconf -fiv && \
	./configure --with-jpeg8 && \
	make && \
	make install

ENV PATH="/opt/mozjpeg/bin/:${PATH}"
ENV GOOGLE_APPLICATION_CREDENTIALS="/go/bin/firestore-db-sa-key.json"
ENV GIT_SSH_COMMAND='ssh -i /go/bin/id_rsa -o StrictHostKeyChecking=no'

RUN git config --global user.email "ntenpas@presencewebservices.net"
RUN git config --global user.name "ntenpas-presence"

WORKDIR /go/bin
COPY --from=go-builder /go/bin/photouploader .
COPY asset-bucket-sa-key.json .
COPY webpic .
COPY new-photogroup .
COPY id_rsa .
EXPOSE 80
ENTRYPOINT ["./photouploader"]
