APP = rectrl_exporter
OCI = podman

REPO ?= "ict.acs.edu/infra"
VERSION = 0.0.1
IMAGE = ${REPO}/${APP}:${VERSION}

build:
	@GOOS=linux GOARCH=amd64 CGO_ENABLED=1 \
	 go build \
	   -trimpath \
	   -mod vendor \
	   -ldflags '-s -w ' \
	   -o bin/${APP}_amd64 main.go
image:
	@${OCI} build -t ${IMAGE} .

clean:
	@rm -rf bin/*
