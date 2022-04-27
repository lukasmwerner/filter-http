FROM golang:alpine AS build

RUN apk add git

WORKDIR /src/

COPY go.* /src/

RUN go mod download -x

COPY . /src

ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64

RUN go build -o /out/app .

FROM scratch AS run

COPY --from=build /out/app /

COPY config.yaml /

EXPOSE 80
EXPOSE 9000

ENTRYPOINT [ "/app" ]