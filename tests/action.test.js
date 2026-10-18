import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { cpSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync, mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const root = new URL("../", import.meta.url);
const action = Bun.YAML.parse(readFileSync(new URL("action.yml", root), "utf8"));
let directory;

beforeEach(() => {
  directory = mkdtempSync(join(tmpdir(), "go-ci-test-"));
  cpSync(new URL("tests/fixture/", root), directory, { recursive: true });
});
afterEach(() => rmSync(directory, { recursive: true }));

function run(name) {
  const step = action.runs.steps.find((candidate) => candidate.name === name);
  return Bun.spawnSync(["bash", "-e", "-o", "pipefail", "-c", step.run], {
    cwd: directory,
    env: { ...process.env, LINT_CONFIG: ".golangci.yml", GOPROXY: "off" },
    stdout: "pipe",
    stderr: "pipe",
  });
}

describe("blocking Go CI checks", () => {
  test("valid source passes every command without creating build artifacts", () => {
    const before = readdirSync(directory).sort();
    for (const step of action.runs.steps.filter((candidate) => candidate.run)) {
      const result = run(step.name);
      if (result.exitCode !== 0) {
        throw new Error(`${step.name} failed:\n${result.stdout.toString()}${result.stderr.toString()}`);
      }
    }
    expect(readdirSync(directory).sort()).toEqual(before);
  });

  test("unformatted source fails formatting", () => {
    writeFileSync(join(directory, "main.go"), "package main\nfunc main(){ }\n");
    expect(run("Format").exitCode).not.toBe(0);
  });

  test("a gofmt error cannot become an empty successful formatting check", () => {
    writeFileSync(join(directory, "main.go"), "not valid Go");
    expect(run("Format").exitCode).not.toBe(0);
  });

  test("an untidy module fails without modifying go.mod", () => {
    const path = join(directory, "go.mod");
    const source = readFileSync(path, "utf8") +
      "\nrequire example.com/unused v0.0.0\nreplace example.com/unused => ./unused\n";
    writeFileSync(path, source);
    mkdirSync(join(directory, "unused"));
    writeFileSync(join(directory, "unused/go.mod"), "module example.com/unused\n\ngo 1.27.2\n");
    expect(run("Tidy modules").exitCode).not.toBe(0);
    expect(readFileSync(path, "utf8")).toBe(source);
  });

  test("vet findings fail CI", () => {
    writeFileSync(join(directory, "main.go"), 'package main\nimport "fmt"\nfunc main() { fmt.Printf("%d", "wrong") }\n');
    expect(run("Vet").exitCode).not.toBe(0);
  });

  test("a failing test fails CI", () => {
    writeFileSync(join(directory, "main_test.go"), 'package main\nimport "testing"\nfunc TestFailure(t *testing.T) { t.Fatal("expected fixture failure") }\n');
    expect(run("Test").exitCode).not.toBe(0);
  });

  test("a build error fails CI", () => {
    writeFileSync(join(directory, "main.go"), "package main\nfunc main() { undefined() }\n");
    expect(run("Build").exitCode).not.toBe(0);
  });

  test("invalid lint configuration fails CI", () => {
    writeFileSync(join(directory, ".golangci.yml"), 'version: "invalid"\n');
    expect(run("Verify lint configuration").exitCode).not.toBe(0);
  });

  test("lint findings fail CI", () => {
    writeFileSync(join(directory, "main.go"), 'package main\nimport "fmt"\nfunc main() { fmt.Printf("%d", "wrong") }\n');
    expect(run("Lint").exitCode).not.toBe(0);
  });

  test("checks cannot be advisory and the action does not tag or release", () => {
    expect(action.runs.steps.every((step) => !step["continue-on-error"])).toBe(true);
    expect(Object.keys(action.inputs)).toEqual(["working-directory", "go-version-file", "golangci-lint-config"]);
    expect(action.runs.steps.filter((step) => step.uses).map((step) => step.uses.split("@")[0]))
      .toEqual(["actions/setup-go", "golangci/golangci-lint-action"]);
  });
});
