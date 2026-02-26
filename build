#!/usr/bin/env bash
# build 2.1.2
set -euo pipefail  # Fail immediately on any error
Gre='\e[1;32m' Red='\e[1;31m' Mag='\e[1;35m' Yel='\e[1;33m' Blu='\e[1;34m' Rst='\e[0m'

# Parse arguments - if provided, only build specified utilities
BUILD_TARGETS=("$@")
Prg=$(head -1 go.mod | awk -F'/' '{print $NF}' | awk '{print $NF}')

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
printf "==> Update go.mod to reflect actual dependencies\ngo mod tidy\n"
go mod tidy

# Format Go code
printf "\n==> Format Go code according to standard rules\ngo fmt $PKG_SCOPE\n"
FMT_OUTPUT=$(go fmt $PKG_SCOPE || true)
if [ -z "$FMT_OUTPUT" ]; then
    printf "    No formatting changes needed.\n"
else
    printf "    $FMT_OUTPUT\n"
fi

# Automatically fix Go code
printf "\n==> Automatically fix code for API/language changes\ngo fix $PKG_SCOPE\n"
FIX_OUTPUT=$(go fix $PKG_SCOPE || true)
if [ -z "$FIX_OUTPUT" ]; then
    printf "    No fixes applied.\n"
else
    printf "    $FIX_OUTPUT\n"
fi

# Vet code
printf "\n==> Check code for potential issues\ngo vet $PKG_SCOPE\n"
VET_OUTPUT=$(go vet $PKG_SCOPE 2>&1 || true)
if [ -z "$VET_OUTPUT" ]; then
    printf "    No issues found by go vet.\n"
else
    printf "    $VET_OUTPUT\n"
fi

# Run tests
printf "\n==> Run tests for all packages in the repository\ngo test $PKG_SCOPE\n"
go test $PKG_SCOPE

# Install staticcheck
printf "\n==> Install static analysis tool for Go\ngo install honnef.co/go/tools/cmd/staticcheck@latest\n"
go install honnef.co/go/tools/cmd/staticcheck@latest

# Run staticcheck
printf "\n==> Analyze Go code for potential issues\nstaticcheck $PKG_SCOPE\n"
staticcheck $PKG_SCOPE

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
        ProgramVersion=$(grep -o 'program_version.*"[^"]*"' "cmd/${UTIL}/main.go" | cut -d'"' -f2 || echo "unknown_version")
        printf "\n==> Building and installing ${Gre}${UTIL} v${ProgramVersion}${Rst}\n"
        (
            set -x
            go build -o "${BINDIR}/${UTIL}${EXT}" -ldflags "-s -w" "$UTIL_DIR"
        )
        printf "    ${Gre}$(ls -l ${BINDIR}/${UTIL}${EXT} | awk '{printf "%'"'"'10d    %s %2s %5s     %s", $5, $6, $7, $8, $9}')${Rst}\n"
        BUILT_COUNT=$((BUILT_COUNT + 1))
    fi
done

# Summary
if [ $BUILT_COUNT -eq 0 ]; then
    printf "\n${Yel}Warning: No utilities were built.${Rst}\n"
    if [ ${#BUILD_TARGETS[@]} -gt 0 ]; then
        printf "Check that the specified utilities exist in ./cmd/\n"
    fi
fi

# Versioning instructions
CurrentTag=$(git tag | sort -V | tail -1)
IFS='.' read -r Major Minor Patch <<< "${CurrentTag#v}"
NextTag="v$Major.$Minor.$((Patch+1))"

printf "\n==> To release as ${Gre}$NextTag${Rst}, adjust comment and run below one-liner:\n"
printf "\n    TAG=${Gre}$NextTag${Rst} && git add . && git commit -m \"${Gre}<insert comment>${Rst}\" && git tag \$TAG && git push origin \$TAG && git push\n\n"

exit 0
