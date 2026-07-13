#!/usr/bin/env bash
# PostToolUse Edit|Write: keep plugin.json and marketplace.json versions in sync.
# plugin.json is authoritative; the marketplace entry must match it.
. "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"

MARKETPLACE="$CLAUDE_PROJECT_DIR/.claude-plugin/marketplace.json"

case "$HOOK_FILE" in
  */plugins/*/.claude-plugin/plugin.json)
    name=$(jq -r '.name' "$HOOK_FILE")
    plugin_v=$(jq -r '.version' "$HOOK_FILE")
    mkt_v=$(jq -r --arg n "$name" '.plugins[] | select(.name == $n) | .version' "$MARKETPLACE")
    if [ -z "$mkt_v" ]; then
      echo "version drift: $name@$plugin_v has no entry in .claude-plugin/marketplace.json — add one" >&2
      exit 2
    fi
    if [ "$plugin_v" != "$mkt_v" ]; then
      echo "version drift: $name is $plugin_v in plugin.json but $mkt_v in marketplace.json — update the marketplace entry to $plugin_v" >&2
      exit 2
    fi
    ;;
  */.claude-plugin/marketplace.json)
    drift=$(
      jq -r '.plugins[] | [.name, .version] | @tsv' "$HOOK_FILE" \
        | while IFS=$'\t' read -r name mkt_v; do
          pj="$CLAUDE_PROJECT_DIR/plugins/$name/.claude-plugin/plugin.json"
          [ -f "$pj" ] || continue
          plugin_v=$(jq -r '.version' "$pj")
          [ "$plugin_v" = "$mkt_v" ] || echo "  $name: plugin.json=$plugin_v marketplace=$mkt_v"
        done
    )
    if [ -n "$drift" ]; then
      printf 'version drift between plugin.json and marketplace.json (plugin.json is authoritative):\n%s\n' "$drift" >&2
      exit 2
    fi
    ;;
esac
