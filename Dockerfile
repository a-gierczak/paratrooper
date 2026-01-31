############################
# STEP 1 build executable binary
############################
FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN apk update
RUN apk add make

RUN CGO_ENABLED=0 make build

############################
# STEP 2 build a small image
############################
FROM scratch

WORKDIR /app

# RUN mkdir -p /tmp

COPY --from=builder /app/bin/server /app/bin/server
COPY --from=builder /app/bin/worker /app/bin/worker
