# imgkit — task runner
#
# `default` is the house self-contained clip-to-width `just --list` (same in every repo,
# public or private). Canonical text + rationale: lockyc-config skill, references/just-pretty.md.

# `default` pipes `just --list` through a small stock-perl filter that clips long recipe
# docs to your terminal width (…) instead of wrapping. Self-contained — no external files;
# falls back to plain `just --list` where perl is absent. Edit the recipes below, not this.
# List available recipes
default:
    @if command -v perl >/dev/null 2>&1; then just --color always --list | perl -CS -Mutf8 -lpe 'BEGIN{($w)=`stty size 2>/dev/null </dev/tty`=~/ (\d+)/; $w||=100; $col=(-t STDOUT && !exists $ENV{NO_COLOR})} s/\e\[[0-9;]*m//g unless $col; (my $v=$_)=~s/\e\[[0-9;]*m//g; if(length($v)>$w){my($o,$n)=("",0); while(length && $n<$w-1){ if($col && s/^(\e\[[0-9;]*m)//){$o.=$1}else{s/^(.)//;$o.=$1;$n++} } $_=$o."…".($col?"\e[0m":"")}'; else just --list; fi

# Build the binary
[group("build")]
build:
    go build -o imgkit .

# Run the test suite
[group("check")]
test:
    go test ./...

# Run the quality cases over quality/ (needs every engine and model; local only)
[group("check")]
quality:
    go test -tags quality -count=1 -timeout 0 -v ./quality/

# go vet static checks
[group("check")]
vet:
    go vet ./...

# Format all Go files in place
[group("check")]
fmt:
    gofmt -w .

# Non-mutating pre-merge gate: gofmt check + vet + tests
[group("check")]
gate:
    #!/usr/bin/env bash
    set -euo pipefail
    unformatted="$(gofmt -l .)"
    if [ -n "$unformatted" ]; then
      echo "✗ gofmt: these files need formatting:" >&2
      echo "$unformatted" >&2
      exit 1
    fi
    go vet ./...
    go vet -tags quality ./quality/
    go test ./...
    echo "✓ gate passed"

# Install imgkit onto GOBIN and print its version
[group("build")]
install:
    #!/usr/bin/env bash
    set -euo pipefail
    go install .
    bin="$(go env GOBIN)"; [ -n "$bin" ] || bin="$(go env GOPATH)/bin"
    "$bin/imgkit" version

# Re-resolve every embedded ML script's lockfile (after editing a PEP 723 header)
[group("build")]
lock-ml:
    for f in internal/ml/scripts/*.py; do uv lock --script "$f"; done
