export default {
  "*.go": "gofmt -w",
  "**/go.mod": (files) => files.map((f) => `go mod edit -fmt ${f}`),
  "!(*.go|**/go.mod)": "prettier --ignore-unknown --write",
};
