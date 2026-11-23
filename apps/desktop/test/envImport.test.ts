// Importação de .env (#18). Roda com `pnpm run test`: node --test, sem dependência nova.
// Todos os valores são falsos e curtos de propósito (não casam com formato de chave real).
// @ts-expect-error sem @types/node, como no vite.config.js
import { test } from "node:test";
// @ts-expect-error sem @types/node
import assert from "node:assert/strict";
// @ts-expect-error sem @types/node
import { readFileSync } from "node:fs";
import { importRows, khRunCommand, parseEnv, planImport } from "../src/lib/envImport.ts";
import type { ImportRow } from "../src/lib/envImport.ts";

const SECRETS = {
  openai: "sk-proj-teste-openai-0001",
  anthropic: "sk-ant-teste-anthropic-0002",
  openrouter: "sk-or-v1-teste-openrouter-0003",
  github: "ghp_teste-github-0004",
};

const ENV = [
  "﻿# chaves do projeto",
  `export OPENAI_API_KEY=${SECRETS.openai}`,
  `ANTHROPIC_API_KEY="${SECRETS.anthropic}"`,
  `OPENROUTER_API_KEY='${SECRETS.openrouter}' # comentário`,
  `GITHUB_TOKEN = ${SECRETS.github}`,
  `GH_TOKEN=${SECRETS.github}`,
  "DATABASE_URL=postgres://localhost/app",
  "NEXT_PUBLIC_FIREBASE_API_KEY=AIza-teste-firebase-0005",
  "STRIPE_SECRET_KEY=teste-stripe-0006",
  "VAZIO_TOKEN=",
].join("\r\n");

test("parseEnv lê o .env como o dotenv", () => {
  const vars = parseEnv(ENV);
  assert.equal(vars.get("OPENAI_API_KEY"), SECRETS.openai); // export + BOM + CRLF
  assert.equal(vars.get("ANTHROPIC_API_KEY"), SECRETS.anthropic); // aspas duplas
  assert.equal(vars.get("OPENROUTER_API_KEY"), SECRETS.openrouter); // aspas simples + comentário
  assert.equal(vars.get("GITHUB_TOKEN"), SECRETS.github); // espaço em volta do =
  assert.equal(vars.has("VAZIO_TOKEN"), false);
  assert.equal(parseEnv("A=1\n# A=2\nA=3").get("A"), "3");
  assert.equal(parseEnv("T=abc== # fim").get("T"), "abc==");
  assert.equal(parseEnv("T=`crase`").get("T"), "crase");
});

test("mapeia cada chave para o provedor e o destino certos", () => {
  const { rows, leftOut } = planImport(ENV, []);
  const by = (alias: string) => rows.find((r) => r.alias === alias);

  assert.deepEqual(by("openai"), {
    names: ["OPENAI_API_KEY"],
    provider: "openai",
    kind: "service",
    baseUrl: "https://api.openai.com",
    alias: "openai",
    masked: "••••••••0001",
  });
  assert.equal(by("anthropic")?.kind, "ai");
  assert.equal(by("anthropic")?.baseUrl, null);
  assert.equal(by("openrouter")?.kind, "ai");
  // A mesma chave em duas variáveis vira uma entrada só, com as duas no kh run
  assert.deepEqual(by("github")?.names, ["GITHUB_TOKEN", "GH_TOKEN"]);
  assert.equal(rows.length, 4);
  // AIza sem nome de Gemini não vira Gemini; o resto fica no .env
  assert.deepEqual(leftOut, ["DATABASE_URL", "NEXT_PUBLIC_FIREBASE_API_KEY", "STRIPE_SECRET_KEY"]);
});

test("o prefixo vence o nome: a chave só vai para quem a emitiu", () => {
  const { rows } = planImport(
    [
      `OPENAI_API_KEY=${SECRETS.openrouter}`, // OpenRouter pelo SDK da OpenAI
      `OPENROUTER_API_KEY=${SECRETS.openai}`,
      "GEMINI_API_KEY=AIza-teste-gemini-0007",
    ].join("\n"),
    [],
  );
  assert.deepEqual(
    rows.map((r) => [r.names[0], r.provider, r.kind]),
    [
      ["OPENAI_API_KEY", "openrouter", "ai"],
      ["OPENROUTER_API_KEY", "openai", "service"],
      ["GEMINI_API_KEY", "gemini", "ai"],
    ],
  );
});

test("nunca reusa um apelido que já existe no cofre", () => {
  const { rows } = planImport(
    `OPENAI_API_KEY=${SECRETS.openai}\nOUTRA=sk-proj-teste-openai-0008\nGH_TOKEN=${SECRETS.github}`,
    ["OpenAI", "github"],
  );
  assert.deepEqual(
    rows.map((r) => r.alias),
    ["openai-2", "openai-3", "github-2"],
  );
});

test("a prévia mostra no máximo os 4 últimos caracteres", () => {
  const { rows } = planImport(`${ENV}\nVERCEL_TOKEN=curtinho`, []);
  assert.equal(rows.find((r) => r.provider === "vercel")?.masked, "••••••••");
  const preview = JSON.stringify(rows);
  for (const secret of Object.values(SECRETS)) assert.equal(preview.includes(secret), false);
});

test("a chave só chega à gravação: nada em console, armazenamento ou rede", async () => {
  const g = globalThis as unknown as Record<string, unknown>;
  const original = { console: g.console, fetch: g.fetch, ls: g.localStorage, ss: g.sessionStorage };
  const vazamentos: string[] = [];
  const espiao = (onde: string) => (...args: unknown[]) => {
    vazamentos.push(`${onde}: ${args.map(String).join(" ")}`);
  };
  const storage = (onde: string) => ({
    setItem: espiao(`${onde}.setItem`),
    getItem: () => null,
    removeItem: () => {},
    clear: () => {},
    key: () => null,
    length: 0,
  });
  g.console = Object.fromEntries(
    ["log", "info", "warn", "error", "debug", "trace"].map((m) => [m, espiao(`console.${m}`)]),
  );
  g.fetch = espiao("fetch");
  g.localStorage = storage("localStorage");
  g.sessionStorage = storage("sessionStorage");

  const gravadas: string[][] = [];
  try {
    const { rows } = planImport(ENV, []);
    const res = await importRows(rows, ENV, async (row, secret) => {
      gravadas.push([row.alias, secret]);
    });
    assert.equal(res.error, "");
    assert.equal(res.done.length, 4);
  } finally {
    g.console = original.console;
    g.fetch = original.fetch;
    g.localStorage = original.ls;
    g.sessionStorage = original.ss;
  }

  assert.deepEqual(vazamentos, []);
  assert.deepEqual(gravadas, [
    ["openai", SECRETS.openai],
    ["anthropic", SECRETS.anthropic],
    ["openrouter", SECRETS.openrouter],
    ["github", SECRETS.github],
  ]);
});

test("para no primeiro erro e diz o que já foi gravado", async () => {
  const { rows } = planImport(ENV, []);
  const tentadas: string[] = [];
  const res = await importRows(rows, ENV, async (row: ImportRow) => {
    tentadas.push(row.alias);
    if (row.alias === "anthropic") throw "Cofre bloqueado";
  });
  assert.deepEqual(
    res.done.map((r) => r.alias),
    ["openai"],
  );
  assert.equal(res.error, "Cofre bloqueado");
  assert.deepEqual(tentadas, ["openai", "anthropic"]);
});

test("monta o kh run para rodar o projeto sem o .env", () => {
  const { rows } = planImport(`OPENAI_API_KEY=${SECRETS.openai}\nGITHUB_TOKEN=${SECRETS.github}\nGH_TOKEN=${SECRETS.github}`, []);
  assert.equal(
    khRunCommand(rows),
    "kh run -e OPENAI_API_KEY=openai -e GITHUB_TOKEN=github -e GH_TOKEN=github -- <comando>",
  );
});

test("a tela não usa armazenamento do navegador nem console", () => {
  for (const file of ["../src/routes/+page.svelte", "../src/lib/envImport.ts", "../src/lib/providers.ts"]) {
    const src = readFileSync(new URL(file, import.meta.url), "utf8");
    assert.doesNotMatch(src, /localStorage|sessionStorage|indexedDB|console\./, file);
  }
});

test("a CSP só deixa a interface falar com ela mesma e com o gateway local", () => {
  const conf = JSON.parse(readFileSync(new URL("../src-tauri/tauri.conf.json", import.meta.url), "utf8"));
  const connect = conf.app.security.csp.split(";").find((d: string) => d.trim().startsWith("connect-src"));
  assert.deepEqual(connect.trim().split(/\s+/).slice(1).sort(), [
    "'self'",
    "http://127.0.0.1:8090",
    "http://ipc.localhost",
    "ipc:",
  ]);
});
