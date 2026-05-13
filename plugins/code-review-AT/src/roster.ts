import { ALWAYS_ON_ROLES, type Role } from "./types.js";

const hasExt = (files: string[], exts: string[]): boolean =>
  files.some((f) => exts.some((ext) => f.toLowerCase().endsWith(ext)));

const hasPath = (files: string[], patterns: RegExp[]): boolean =>
  files.some((f) => patterns.some((re) => re.test(f)));

const TS_EXTS = [".ts", ".tsx", ".cts", ".mts"];
const REACT_EXTS = [".tsx", ".jsx"];
const REACT_PATHS = [
  /(^|\/)(components|pages|app|src\/app|src\/components|src\/pages)\//i,
];
const INFRA_PATHS = [
  /\.sql$/i,
  /(^|\/)migrations\//i,
  /(^|\/)db\/migrations\//i,
  /\.tf$/i,
  /\.hcl$/i,
  /(^|\/)terraform\//i,
  /(^|\/)Dockerfile(\..+)?$/i,
  /(^|\/)docker-compose(\..+)?$/i,
  /(^|\/)k8s\//i,
  /(^|\/)kubernetes\//i,
  /(^|\/)helm\//i,
  /(^|\/)deploy\//i,
  /(^|\/)infra(structure)?\//i,
];

export const buildRoster = (
  changedFiles: string[],
  claudeMdFiles: string[],
): Role[] => {
  const roster = new Set<Role>(ALWAYS_ON_ROLES);
  if (hasExt(changedFiles, TS_EXTS)) roster.add("typescript");
  if (hasExt(changedFiles, REACT_EXTS) || hasPath(changedFiles, REACT_PATHS))
    roster.add("react");
  if (hasPath(changedFiles, INFRA_PATHS)) roster.add("infra");
  if (claudeMdFiles.length > 0) roster.add("claude-md");
  return [...roster];
};
