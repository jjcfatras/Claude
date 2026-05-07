export default {
  "*.go": "gofmt -w",
  "**/go.mod": (files) => files.map((f) => `go mod edit -fmt ${f}`),
  "*": "prettier --ignore-unknown --write",
};
