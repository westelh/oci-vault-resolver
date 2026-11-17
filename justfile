RESOLVER_PLATFORMS := env("RESOLVER_PLATFORMS", "amd64 arm64")
RESOLVER_BINARY_DIR := env("RESOLVER_BINARY_DIR", "dist")
RESOLVER_VERSION := env("RESOLVER_VERSION", "dev")

# 引数なしの `just` で一覧を表示する。
[private]
default:
    @just --justfile "{{ justfile() }}" --list

# gofmt -l は未整形のファイルがあっても成功するので、出力の有無で判定する。
# go.mod の toolchain に揃えるため、PATH の gofmt ではなく go env GOROOT の gofmt を使う。
[doc('Go コードの書式を gofmt で検査する')]
[script]
fmt-check:
    unformatted="$("$(go env GOROOT)/bin/gofmt" -l .)"
    if [ -n "$unformatted" ]; then
      echo "gofmt で整形されていないファイルがあります（gofmt -w で整形してください）:"
      echo "$unformatted"
      exit 1
    fi

[doc('Go コードを go vet で検査する')]
vet:
    go vet ./...

[doc('Go テストを実行する')]
test:
    go test ./...

[doc('RESOLVER_PLATFORMS 向けに静的バイナリをビルドする')]
[script]
build:
    mkdir -p "{{ RESOLVER_BINARY_DIR }}"
    for arch in {{ RESOLVER_PLATFORMS }}; do
      echo "Building oci-vault-resolver for linux/$arch"
      CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath \
        -ldflags="-s -w -buildid= -X main.version={{ RESOLVER_VERSION }}" \
        -o "{{ RESOLVER_BINARY_DIR }}/oci-vault-resolver-linux-$arch" .
    done
