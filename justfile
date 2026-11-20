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

# go mod verify は go.sum と照らしてモジュールキャッシュの改ざんを見つける。
# govulncheck 用の tools/go.mod も Dependabot で更新されるので、同じように検査する。
[doc('go.mod と go.sum のずれを go mod tidy -diff で、モジュールキャッシュの改ざんを go mod verify で検査する')]
mod-check:
    go mod tidy -diff
    go mod verify
    cd tools && go mod tidy -diff
    cd tools && go mod verify

[doc('Go コードを go vet で検査する')]
vet:
    go vet ./...

# 設定は .golangci.yml。
[doc('Go コードを golangci-lint で検査する')]
lint:
    golangci-lint run ./...

# アプリのモジュールとは別の go.mod にするのは、govulncheck の依存がアプリの依存の版を引き上げないようにするため。
# 脆弱性データベース（vuln.go.dev）の取得にネットワークが要るので、hk には入れず CI だけで実行する。
[doc('コードから到達できる既知の脆弱性を govulncheck で検査する')]
vuln:
    go tool -modfile=tools/go.mod govulncheck ./...

# run: のシェルは PATH の shellcheck で検査する。
[doc('GitHub Actions のワークフローを actionlint で検査する')]
actionlint:
    actionlint

# GH_TOKEN か GITHUB_TOKEN があるとオンラインの監査（impostor-commit など）も行い、ないときはオフラインで動く。
[doc('GitHub Actions のワークフローと Dependabot の設定を zizmor で検査する')]
zizmor:
    zizmor .github

[doc('コード・コメント・ドキュメントのスペルミスを typos で検査する')]
typos:
    typos

# -race は cgo が要るので CGO_ENABLED=1 を明示する。
# -shuffle=on は失敗したパッケージの出力に -test.shuffle <seed> を出すので、-shuffle=<seed> で順序を再現できる。
[doc('Go テストを -race と -shuffle=on で実行する')]
test:
    CGO_ENABLED=1 go test -race -shuffle=on ./...

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

# package.json の skills に宣言した他リポジトリの Skill を同期する。
# 同期したファイルと skills-lock.json をリポジトリに置くのは、クラウドセッションの開始時にネットワークなしで読み込めるようにするため。

# npx --no-install は devDependencies で固定した skills CLI を使い、別バージョンを取得しない。
# DISABLE_TELEMETRY=1 で CLI の利用統計の送信を止める。
[doc('package.json の skills を同期し、変更されたファイルを表示する')]
skills-sync:
    npm ci
    DISABLE_TELEMETRY=1 npx --no-install skills experimental_sync --agent claude-code --yes
    git status --short -- skills-lock.json .agents/skills .claude/skills
