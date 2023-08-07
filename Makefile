APP = rectrl_exporter
build:
	@GOOS=linux GOARCH=amd64 CGO_ENABLED=1 \
	 go build \
	   -trimpath \
	   -mod vendor \
	   -ldflags '-s -w ' \
	   -o bin/${APP}_amd64 main.go