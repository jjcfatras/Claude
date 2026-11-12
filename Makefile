# Repo tasks (formerly package.json scripts). Run as `make <target>`.
# `./tools/...`, not `./...`: the root module would also walk gitignored *-workspace/ scratch packages.
GO_MODULES := plugins/pr-review/tools/code-review-helper .claude/skills/plugin-session-auditor/tools/session-parser

.PHONY: format lint-go build-go test validate-plugins hooks

format:
	scripts/format.sh

lint-go:
	go vet ./tools/...
	for d in $(GO_MODULES); do go -C "$$d" vet ./... || exit 1; done

build-go:
	$(MAKE) -C plugins/pr-review/tools/code-review-helper release

test:
	go test ./tools/...
	$(MAKE) -C plugins/pr-review/tools/code-review-helper test

validate-plugins:
	claude plugin validate --strict . && for p in plugins/*/; do claude plugin validate --strict "$$p" || exit 1; done

hooks:
	go tool lefthook install
