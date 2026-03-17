#!/usr/bin/env bash
# build 2.2.0

set -euo pipefail  # Fail immediately on any error
Gre='\e[1;32m' Red='\e[1;31m' Mag='\e[1;35m' Yel='\e[1;33m' Blu='\e[1;34m' Rst='\e[0m'

# Pre-scan for -v / --verbose before positional arg parsing so it never
# lands in BUILD_TARGETS and the release syntax ./build.sh TAG MSG is unchanged.
VERBOSE=false
FILTERED_ARGS=()
for arg in "$@"; do
    if [[ "$arg" == "-v" || "$arg" == "--verbose" ]]; then
        VERBOSE=true
    else
        FILTERED_ARGS+=("$arg")
    fi
done
set -- "${FILTERED_ARGS[@]}"

# Parse arguments
#   [v1.2.3] [message]  optional: release tag + commit message for the one-liner
#   remaining positional args → build targets (unchanged behaviour)
TAG_ARG=""
MSG_ARG=""
BUILD_TARGETS=()
if [[ $# -ge 1 ]] && [[ "$1" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    TAG_ARG="$1"; shift
    if [[ $# -ge 1 ]]; then MSG_ARG="$1"; shift; fi
    BUILD_TARGETS=("$@")
else
    BUILD_TARGETS=("$@")
fi
Prg=$(head -1 go.mod | awk -F'/' '{print $NF}' | awk '{print $NF}')

# indent pipes each line through sed to add 4-space prefix and highlight FAILs in red.
indent() { sed "s/^/    /" | sed "s/\(.*FAIL.*\)/$(printf '\e[1;31m')\1$(printf '\e[0m')/"; }

# Detect OS
case "$OSTYPE" in
"linux-gnu"* ) printf "==> Linux\n" && BINDIR=$GOPATH/bin && EXT="" ;;
"darwin"* )    printf "==> macOS\n" && BINDIR=$GOPATH/bin && EXT="" ;;
"msys"* )      printf "==> Windows with GitBASH\n" && BINDIR=$GOPATH/bin && EXT=".exe" ;;
* )            printf "==> Unknown OS '$OSTYPE'. Aborting.\n" && exit 1 ;;
esac

# Determine package scope
if [ ${#BUILD_TARGETS[@]} -eq 0 ]; then
    PKG_SCOPE="./..."
else
    PKG_SCOPE=""
    for target in "${BUILD_TARGETS[@]}"; do
        PKG_SCOPE="$PKG_SCOPE ./cmd/$target"
    done
fi

# Update dependencies
printf "==> Update go.mod to reflect actual dependencies\n"
printf "    ${Gre}go mod tidy${Rst}\n"
go mod tidy

# Format Go code
printf "\n==> Format Go code according to standard rules\n"
printf "    ${Gre}go fmt $PKG_SCOPE${Rst}\n"
FMT_OUTPUT=$(go fmt $PKG_SCOPE || true)
if [ -z "$FMT_OUTPUT" ]; then
    printf "    No formatting changes needed.\n"
else
    printf "%s\n" "$FMT_OUTPUT" | indent
fi

# Automatically fix Go code
printf "\n==> Automatically fix code for API/language changes\n"
printf "    ${Gre}go fix $PKG_SCOPE${Rst}\n"
FIX_OUTPUT=$(go fix $PKG_SCOPE || true)
if [ -z "$FIX_OUTPUT" ]; then
    printf "    No fixes applied.\n"
else
    printf "%s\n" "$FIX_OUTPUT" | indent
fi

# Vet code
printf "\n==> Check code for potential issues\n"
printf "    ${Gre}go vet $PKG_SCOPE${Rst}\n"
VET_OUTPUT=$(go vet $PKG_SCOPE 2>&1 || true)
if [ -z "$VET_OUTPUT" ]; then
    printf "    No issues found by go vet.\n"
else
    printf "%s\n" "$VET_OUTPUT" | indent
fi

# Run tests — check Go 1.20+ for accurate multi-package coverage profiles
GO_VER=$(go version | awk '{print $3}')
GO_MINOR=$(echo "$GO_VER" | sed 's/go1\.\([0-9]*\).*/\1/')
if [[ -z "$GO_MINOR" || "$GO_MINOR" -lt 20 ]]; then
    printf "    ${Yel}Warning: %s detected; -coverprofile multi-package merge requires Go 1.20+. Coverage total may be inaccurate.${Rst}\n" "$GO_VER"
fi
COVER_FILE=$(mktemp /tmp/iq_cover_XXXXXX.out)
trap "rm -f \"$COVER_FILE\"" EXIT
printf "\n==> Run tests for all packages in the repository\n"
if $VERBOSE; then
    printf "    ${Gre}go test -v -coverprofile=$COVER_FILE $PKG_SCOPE${Rst}\n"
    go test -v -coverprofile="$COVER_FILE" $PKG_SCOPE 2>&1 | indent
else
    printf "    ${Gre}go test -coverprofile=$COVER_FILE $PKG_SCOPE${Rst}\n"
    go test -coverprofile="$COVER_FILE" $PKG_SCOPE 2>&1 | indent
fi

# Coverage summary
# Domain coverage (internal/* only) is the meaningful signal — cmd/iq is
# integration-heavy CLI code with a structural ceiling of ~10-15%.
TOTAL_LINE=$(go tool cover -func="$COVER_FILE" 2>/dev/null | grep "^total:" || true)
if [ -n "$TOTAL_LINE" ]; then
    TOTAL_PCT=$(echo "$TOTAL_LINE" | awk '{print $NF}' | tr -d '%')
    DOMAIN_PCT=$(grep "^${Prg}/internal/" "$COVER_FILE" 2>/dev/null \
        | awk '{t+=$2; if($3>0) c+=$2} END{if(t>0) printf "%.1f", c/t*100; else print "0"}' || true)
    DOM_INT=${DOMAIN_PCT%.*}
    if [ "$DOM_INT" -ge 75 ]; then
        COV_COLOR=$Gre
    elif [ "$DOM_INT" -ge 50 ]; then
        COV_COLOR=$Yel
    else
        COV_COLOR=$Red
    fi
    printf "    ${COV_COLOR}domain coverage: ${DOMAIN_PCT}%%${Rst}  ${Mag}(total: ${TOTAL_PCT}%%)${Rst}\n"
fi

# Install staticcheck
printf "\n==> Install static analysis tool for Go\n"
printf "    ${Gre}go install honnef.co/go/tools/cmd/staticcheck@latest${Rst}\n"
go install honnef.co/go/tools/cmd/staticcheck@latest

# Run staticcheck
printf "\n==> Analyze Go code for potential issues\n"
printf "    ${Gre}staticcheck $PKG_SCOPE${Rst}\n"
SC_OUTPUT=$(staticcheck $PKG_SCOPE 2>&1 || true)
if [ -z "$SC_OUTPUT" ]; then
    printf "    No issues found by staticcheck.\n"
else
    printf "%s\n" "$SC_OUTPUT" | indent
fi

# Function to check if a utility should be built
should_build() {
    local util=$1
    if [ ${#BUILD_TARGETS[@]} -eq 0 ]; then
        return 0
    fi
    for target in "${BUILD_TARGETS[@]}"; do
        if [ "$target" = "$util" ]; then
            return 0
        fi
    done
    return 1
}

# Display build mode
if [ ${#BUILD_TARGETS[@]} -eq 0 ]; then
    printf "\n==> Building ${Gre}all utilities${Rst}\n"
else
    printf "\n==> Building ${Gre}specific utilities: ${BUILD_TARGETS[*]}${Rst}\n"
fi

# Build each utility
BUILT_COUNT=0
for UTIL_DIR in ./cmd/*; do
    if [ -d "$UTIL_DIR" ]; then
        UTIL=$(basename "$UTIL_DIR")
        if ! should_build "$UTIL"; then
            continue
        fi
        ProgramVersion=$(grep -o 'programVersion.*"[^"]*"' "cmd/${UTIL}/main.go" | cut -d'"' -f2 || echo "unknown_version")
        printf "\n==> Building and installing ${Gre}${UTIL} v${ProgramVersion}${Rst}\n"
        printf "    ${Gre}go build -o ${BINDIR}/${UTIL}${EXT} -ldflags \"-s -w\" $UTIL_DIR${Rst}\n"
        go build -o "${BINDIR}/${UTIL}${EXT}" -ldflags "-s -w" "$UTIL_DIR"
        printf "    ${Gre}$(ls -l ${BINDIR}/${UTIL}${EXT} | awk '{printf "%'"'"'10d    %s %2s %5s     %s", $5, $6, $7, $8, $9}')${Rst}\n"
        BUILT_COUNT=$((BUILT_COUNT + 1))
    fi
done

# Summary
if [ $BUILT_COUNT -eq 0 ]; then
    printf "\n${Yel}Warning: No utilities were built.${Rst}\n"
    if [ ${#BUILD_TARGETS[@]} -gt 0 ]; then
        printf "    Check that the specified utilities exist in ./cmd/\n"
    fi
fi

# Versioning instructions
CurrentTag=$(git tag | sort -V | tail -1)
IFS='.' read -r Major Minor Patch <<< "${CurrentTag#v}"
NextTag="v$Major.$Minor.$((Patch+1))"

if [ -n "$TAG_ARG" ] && [ -n "$MSG_ARG" ]; then
    printf "\n==> Releasing as ${Gre}$TAG_ARG${Rst}:\n"
    printf "\n    git add . && git commit -m %s && git tag %s && git push origin %s && git push\n\n" \
        "$(printf '%q' "$MSG_ARG")" "$TAG_ARG" "$TAG_ARG"
    git add .
    git commit -m "$MSG_ARG"
    git tag "$TAG_ARG"
    git push origin "$TAG_ARG"
    git push
else
    printf "\n==> To release, adjust comment and run:\n\n    ./build.sh ${Gre}$NextTag${Rst} \"${Gre}<insert comment>${Rst}\"\n\n"
fi

exit 0
