#!/usr/bin/env bash
# Print the repo's default branch: origin/HEAD, else the first of main/master
# that exists on origin or locally, else main.
set -u

if ref=$(git symbolic-ref --short refs/remotes/origin/HEAD 2> /dev/null); then
  echo "${ref#origin/}"
  exit 0
fi

for b in main master; do
  if git rev-parse --verify --quiet "refs/remotes/origin/$b" > /dev/null \
    || git rev-parse --verify --quiet "refs/heads/$b" > /dev/null; then
    echo "$b"
    exit 0
  fi
done

echo main
