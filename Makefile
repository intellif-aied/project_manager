VERSION := $(shell cat VERSION 2>/dev/null || echo 0.1.0)
GO_DOCKER_IMAGE ?= golang:1.26-alpine
SHA256SUM ?= $(shell command -v sha256sum >/dev/null 2>&1 && echo sha256sum || echo "shasum -a 256")

TEST_AIDA_RELEASE_URL ?= http://192.168.14.157:9000/statics-live/aida
TEST_AIDA_API_URL ?= http://192.168.14.157:18090/api/v1
TEST_AIDA_INTERNAL_API_URL ?= http://192.168.14.157:18090/api/v1
AIDA_API_URL ?= http://localhost:8080/api/v1
AIDA_INTERNAL_API_URL ?= http://192.168.14.182/api/v1

.PHONY: version build-release-binaries release-dir release-test-dir release-prod-dir release-test-archive release-prod-archive clean

version:
	@echo "aida v$(VERSION)"

build-release-binaries:
	mkdir -p dist
	mkdir -p "$(HOME)/go/pkg/mod" "$(HOME)/.cache/go-build"
	docker run --rm \
		-v "$(CURDIR):/app" \
		-v "$(HOME)/go/pkg/mod:/go/pkg/mod" \
		-v "$(HOME)/.cache/go-build:/root/.cache/go-build" \
		-w /app/daemon \
		$(GO_DOCKER_IMAGE) \
		sh -c 'CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w -X main.Version=$(VERSION) -X main.DefaultReleaseURL=$(RELEASE_URL)" -o /app/dist/aida-linux-amd64 . && \
		       CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "-s -w -X main.Version=$(VERSION) -X main.DefaultReleaseURL=$(RELEASE_URL)" -o /app/dist/aida-darwin-arm64 . && \
		       CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "-s -w -X main.Version=$(VERSION) -X main.DefaultReleaseURL=$(RELEASE_URL)" -o /app/dist/aida-darwin-amd64 . && \
		       CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-s -w -X main.Version=$(VERSION) -X main.DefaultReleaseURL=$(RELEASE_URL)" -o /app/dist/aida-windows-amd64.exe .'

# Backward-compatible default: build the local test release package.
release-dir: release-test-dir

release-test-dir: RELEASE_URL=$(TEST_AIDA_RELEASE_URL)
release-test-dir: build-release-binaries
	$(call package_release,aida-releases-test,$(TEST_AIDA_RELEASE_URL),$(TEST_AIDA_API_URL),$(TEST_AIDA_INTERNAL_API_URL))

release-prod-dir: RELEASE_URL=$(AIDA_RELEASE_URL)
release-prod-dir: build-release-binaries
	$(if $(AIDA_RELEASE_URL),,$(error AIDA_RELEASE_URL is required. Example: make release-prod-dir AIDA_RELEASE_URL=http://<server>:9000/statics-live/aida AIDA_API_URL=http://<server>:18090/api/v1))
	$(call package_release,aida-releases-release,$(AIDA_RELEASE_URL),$(AIDA_API_URL),$(AIDA_INTERNAL_API_URL))

release-test-archive: release-test-dir
	tar -czf aida_release_$(VERSION)_test_multi_platform.tar.gz -C aida-releases-test .
	@ls -lh aida_release_$(VERSION)_test_multi_platform.tar.gz

release-prod-archive: release-prod-dir
	tar -czf aida_release_$(VERSION)_release_multi_platform.tar.gz -C aida-releases-release .
	@ls -lh aida_release_$(VERSION)_release_multi_platform.tar.gz

define package_release
	rm -rf "$(1)"
	mkdir -p "$(1)"
	cp dist/aida-linux-amd64 "$(1)/aida-linux-amd64"
	cp dist/aida-darwin-arm64 "$(1)/aida-darwin-arm64"
	cp dist/aida-darwin-amd64 "$(1)/aida-darwin-amd64"
	cp dist/aida-windows-amd64.exe "$(1)/aida-windows-amd64.exe"
	cp install.sh "$(1)/install.sh"
	cp install.ps1 "$(1)/install.ps1"
	perl -0pi -e 's|AIDA_RELEASE_URL:-[^}]*|AIDA_RELEASE_URL:-$(2)|' "$(1)/install.sh"
	perl -0pi -e 's|AIDA_API_URL:-[^}]*|AIDA_API_URL:-$(3)|' "$(1)/install.sh"
	perl -0pi -e 's|AIDA_INTERNAL_API_URL:-[^}]*|AIDA_INTERNAL_API_URL:-$(4)|' "$(1)/install.sh"
	perl -0pi -e 's|^\$$DefaultReleaseUrl = .*|\$$DefaultReleaseUrl = "$(2)"|m' "$(1)/install.ps1"
	perl -0pi -e 's|^\$$DefaultApiUrl = .*|\$$DefaultApiUrl = "$(3)"|m' "$(1)/install.ps1"
	perl -0pi -e 's|^\$$DefaultInternalApiUrl = .*|\$$DefaultInternalApiUrl = "$(4)"|m' "$(1)/install.ps1"
	chmod 755 "$(1)/install.sh"
	echo "$(VERSION)" > "$(1)/aida-latest.txt"
	cd "$(1)" && $(SHA256SUM) aida-linux-amd64 aida-darwin-arm64 aida-darwin-amd64 aida-windows-amd64.exe install.sh install.ps1 aida-latest.txt > SHA256SUMS.txt
	@echo "release directory ready: ./$(1)"
	@echo "publish its contents to: $(2)"
	@echo "install command:"
	@echo "  curl -fsSL $(2)/install.sh | bash"
	@echo "  aida login"
	@echo "  Invoke-RestMethod $(2)/install.ps1 | Invoke-Expression"
endef

clean:
	rm -rf dist aida-releases-test aida-releases-release aida_release_*.tar.gz
