---
paths:
  - "**/CLAUDE.md"
  - "**/CLAUDE.local.md"
  - ".claude/rules/**"
---

# CLAUDE.md structure

Write every instruction file to Anthropic's [CLAUDE.md files](https://code.claude.com/docs/en/memory#claude-md-files) guidance, plus the [Keep CLAUDE.md short](https://code.claude.com/docs/en/best-practices#keep-claude-md-short) and [write effective instructions](https://code.claude.com/docs/en/memory#write-effective-instructions) best practices. Claude treats these files as [context, not enforced configuration](https://code.claude.com/docs/en/memory#claude-md-vs-auto-memory): they are [guidelines Claude may decide don't apply](https://code.claude.com/docs/en/best-practices#enforce-the-rules-that-cant-have-exceptions).

## Where each file goes

| File                                       | Holds                                                                                                                                                                                                                                                                                                       |
| ------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `./CLAUDE.md` or `./.claude/CLAUDE.md`     | Team-shared project instructions, committed ([scopes](https://code.claude.com/docs/en/memory#choose-where-to-put-claude-md-files))                                                                                                                                                                          |
| `./CLAUDE.local.md`                        | Personal project preferences. Add it to `.gitignore` ([scopes](https://code.claude.com/docs/en/memory#choose-where-to-put-claude-md-files))                                                                                                                                                                 |
| `~/.claude/CLAUDE.md`, managed policy file | Personal preferences for all projects; organization-wide policy ([scopes](https://code.claude.com/docs/en/memory#choose-where-to-put-claude-md-files))                                                                                                                                                      |
| `.claude/rules/*.md`                       | One topic per file. With no `paths:` it loads at launch like `.claude/CLAUDE.md` ([set up rules](https://code.claude.com/docs/en/memory#set-up-rules)); with `paths:` it loads only when Claude touches a matching file ([path-specific rules](https://code.claude.com/docs/en/memory#path-specific-rules)) |
| `~/.claude/rules/*.md`                     | Personal rules for every project. They load before project rules, and neither set overrides the other ([user-level rules](https://code.claude.com/docs/en/memory#user-level-rules))                                                                                                                         |
| `.claude/skills/<name>/SKILL.md`           | Multi-step procedures. Claude reads one only when the task calls for it ([when to add](https://code.claude.com/docs/en/memory#when-to-add-to-claude-md), [skills](https://code.claude.com/docs/en/best-practices#put-team-procedures-in-skills)). Layout: [skill-structure.md](skill-structure.md)          |

## How files load

- `CLAUDE.md` and `CLAUDE.local.md` in the working directory and every directory above it load at launch. Files in subdirectories load once Claude reads, writes or edits a file there ([how files load](https://code.claude.com/docs/en/memory#how-claude-md-files-load)).
- All files are concatenated, not overridden. They are ordered from the filesystem root down, and `CLAUDE.local.md` comes after `CLAUDE.md` in each directory ([how files load](https://code.claude.com/docs/en/memory#how-claude-md-files-load)). When two instructions contradict each other, Claude may pick either one, so keep the files consistent ([write effective instructions](https://code.claude.com/docs/en/memory#write-effective-instructions), [user-level rules](https://code.claude.com/docs/en/memory#user-level-rules)).
- The project-root `CLAUDE.md` is re-read and re-injected after `/compact`. Nested files and path-scoped rules load again on demand ([what survives compaction](https://code.claude.com/docs/en/context-window#what-survives-compaction)).
- Block-level HTML comments (`<!-- … -->`) are stripped before injection. Use them for notes to human maintainers ([how files load](https://code.claude.com/docs/en/memory#how-claude-md-files-load)).

## What belongs

Only write down what Claude can't find by reading the repo, or would otherwise rediscover every session. If Claude could find a fact in the code and would actually go and look, leave it out ([tell Claude what it can't see](https://code.claude.com/docs/en/best-practices#tell-claude-what-it-cant-see)).

- **Include**: the exact test, typecheck and lint commands, and how to run a single test file. Also places where the code misleads, decisions the repo doesn't record, and anything Claude has had to be corrected on twice ([keep CLAUDE.md short](https://code.claude.com/docs/en/best-practices#keep-claude-md-short)). Add an entry when Claude repeats a mistake or a review catches something it should have known ([when to add](https://code.claude.com/docs/en/memory#when-to-add-to-claude-md)).
- **Leave out** descriptions of architecture and file layout, because Claude reads the repo itself ([what you can stop doing](https://code.claude.com/docs/en/best-practices#what-you-can-stop-doing)). `/doctor` cuts directory layouts, dependency lists and architecture overviews for the same reason ([too large](https://code.claude.com/docs/en/memory#my-claude-md-is-too-large)).
- **Move out**:
  - procedures go to a skill ([skills](https://code.claude.com/docs/en/best-practices#put-team-procedures-in-skills));
  - rules for one part of the codebase go to a `paths:` rule ([scope rules](https://code.claude.com/docs/en/best-practices#scope-rules-to-one-part-of-the-codebase));
  - rules that can't have exceptions go to a hook, deny rule or managed setting, because a CLAUDE.md line is a guideline Claude may judge not to apply ([deterministic vs non-deterministic](https://code.claude.com/docs/en/best-practices#deterministic-and-non-deterministic-instructions), [enforce the rules](https://code.claude.com/docs/en/best-practices#enforce-the-rules-that-cant-have-exceptions));
  - personal preferences go to `CLAUDE.local.md` ([keep CLAUDE.md short](https://code.claude.com/docs/en/best-practices#keep-claude-md-short)).

A committed `CLAUDE.md` or rule should never reference `CLAUDE.local.md`: the file is gitignored ([import additional files](https://code.claude.com/docs/en/memory#import-additional-files)), so the pointer dangles for everyone else.

## Writing

- Write instructions concrete enough to verify: "Use 2-space indentation", not "Format code properly" ([write effective instructions](https://code.claude.com/docs/en/memory#write-effective-instructions)).
- Target under 200 lines per file. Longer files consume more context and reduce adherence ([write effective instructions](https://code.claude.com/docs/en/memory#write-effective-instructions)). Claude Code skips any file over 4 MiB ([too large](https://code.claude.com/docs/en/memory#my-claude-md-is-too-large)).
- Group related instructions under Markdown headers and bullets, not dense paragraphs ([write effective instructions](https://code.claude.com/docs/en/memory#write-effective-instructions)).
- No IMPORTANT or ALL CAPS. Current models follow a plainly worded line, and extra emphasis makes Claude over-apply the rule. If a line keeps getting missed, shorten the file; if it is a rule that can't have exceptions, turn it into a hook ([what you can stop doing](https://code.claude.com/docs/en/best-practices#what-you-can-stop-doing), [keep CLAUDE.md short](https://code.claude.com/docs/en/best-practices#keep-claude-md-short)).

## Imports

- `@path/to/file` expands the file into context. A relative path resolves from the importing file, and imports nest at most four hops deep ([imports](https://code.claude.com/docs/en/memory#import-additional-files)).
- A path inside a code span or fenced block is not imported. Write file paths in backticks to keep them literal ([imports](https://code.claude.com/docs/en/memory#import-additional-files)).
- Imports organize a long file but don't reduce its context cost: imported files load with the file that imports them ([write effective instructions](https://code.claude.com/docs/en/memory#write-effective-instructions)).
- An import in a project-level file that resolves outside the working directory shows a one-time approval dialog per project; imports in `~/.claude/CLAUDE.md` and `~/.claude/rules/` load without it ([imports](https://code.claude.com/docs/en/memory#import-additional-files)).

## Rule files

- Give each file one topic and a descriptive filename (`testing.md`, `api-design.md`). Files are discovered recursively ([set up rules](https://code.claude.com/docs/en/memory#set-up-rules)).
- Claude Code reads only `paths` from a rule's frontmatter and ignores every other field. It strips the frontmatter before loading, and if the YAML doesn't parse, the rule loads as if it had no `paths` ([rule frontmatter](https://code.claude.com/docs/en/memory#rule-frontmatter-reference)). Glob and brace-expansion semantics are in [path-specific rules](https://code.claude.com/docs/en/memory#path-specific-rules).
- Claude receives only the start of a rule that loads on demand and is over the per-file limit. The limit is 40,000 characters on a 200K context window. Check with `wc -m` ([subdirectory limit](https://code.claude.com/docs/en/memory#claude-ignores-the-end-of-my-subdirectory-claude-md)).

## Validate

- `/context all` lists the files that loaded at launch under **Memory files** ([set up a project CLAUDE.md](https://code.claude.com/docs/en/memory#set-up-a-project-claude-md)).
- `/doctor prompt-audit` reports instructions that are stale, point at missing files or contradict each other ([audit](https://code.claude.com/docs/en/memory#audit-your-instruction-files)).
- The `InstructionsLoaded` hook logs which files and rules load, and when ([troubleshoot](https://code.claude.com/docs/en/memory#troubleshoot-memory-issues)).

For `AGENTS.md`, `claudeMdExcludes`, managed `claudeMd` and auto memory, see [AGENTS.md](https://code.claude.com/docs/en/memory#agents-md) and the rest of that page.
