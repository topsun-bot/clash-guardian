.PHONY: test build clean

test:
	go test -race ./...

build:
	mkdir -p dist
	GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o dist/clash-guardian-darwin-arm64 .
	GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/clash-guardian-darwin-amd64 .
	GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o dist/clash-guardian-linux-arm64 .
	GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/clash-guardian-linux-amd64 .
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/clash-guardian-windows-amd64.exe .

clean:
	rm -rf dist

