// Backup do cofre (#26). Roda com `pnpm run test`: node --test, sem dependência nova.
// Senhas são falsas e só servem para medir tamanho e confirmação.
// @ts-expect-error sem @types/node, como no envImport.test.ts
import { test } from "node:test";
// @ts-expect-error sem @types/node
import assert from "node:assert/strict";
// @ts-expect-error sem @types/node
import { readFileSync } from "node:fs";
import {
  canConfirmReplace,
  defaultBackupName,
  formatBackupDate,
  passphraseStrength,
  validatePassphrase,
} from "../src/lib/backup.ts";

test("o nome sugerido usa o horário local, com zeros à esquerda", () => {
  // Mês é 0-indexado: 10 é novembro
  assert.equal(defaultBackupName(new Date(2026, 10, 26, 9, 5, 3)), "keyhybrid-backup-20261126-090503.khvault");
  assert.equal(defaultBackupName(new Date(2026, 0, 1, 23, 59, 59)), "keyhybrid-backup-20260101-235959.khvault");
});

test("a força começa em 'Curta demais' abaixo de 12 caracteres", () => {
  assert.deepEqual(passphraseStrength(""), { score: 0, label: "Curta demais" });
  assert.deepEqual(passphraseStrength("a".repeat(11)), { score: 0, label: "Curta demais" });
  // Acentos contam como um caractere cada, não como bytes
  assert.deepEqual(passphraseStrength("ç".repeat(11)), { score: 0, label: "Curta demais" });
});

test("a força sobe com o tamanho e com a variedade de tipos", () => {
  assert.deepEqual(passphraseStrength("abcdefghijkl"), { score: 1, label: "Fraca" });
  assert.deepEqual(passphraseStrength("Abcdefghijk1"), { score: 2, label: "Razoável" });
  assert.deepEqual(passphraseStrength("Abcdefghijklmnop1"), { score: 3, label: "Boa" });
  assert.deepEqual(passphraseStrength("Abcdefghijklmnopqrst1!"), { score: 4, label: "Forte" });
});

test("a força conta caracteres Unicode, não bytes", () => {
  // 12 emojis são 12 caracteres: aceitos pelo mínimo
  assert.equal(passphraseStrength("🔐".repeat(12)).label, "Fraca");
  assert.equal(passphraseStrength("ã".repeat(12)).label, "Fraca");
});

test("recusa senha curta e senhas que não batem", () => {
  assert.equal(validatePassphrase("curta", "curta"), "A senha do backup precisa de pelo menos 12 caracteres.");
  assert.equal(validatePassphrase("ç".repeat(11), "ç".repeat(11)), "A senha do backup precisa de pelo menos 12 caracteres.");
  assert.equal(validatePassphrase("abcdefghijkl", "abcdefghijkx"), "As duas senhas não são iguais.");
  assert.equal(validatePassphrase("abcdefghijkl", "abcdefghijkl"), null);
  assert.equal(validatePassphrase("🔐".repeat(12), "🔐".repeat(12)), null);
});

test("SUBSTITUIR só vale com a palavra exata (espaços das pontas não contam)", () => {
  assert.equal(canConfirmReplace("SUBSTITUIR"), true);
  assert.equal(canConfirmReplace(" SUBSTITUIR "), true);
  assert.equal(canConfirmReplace("substituir"), false);
  assert.equal(canConfirmReplace("SUBSTITUIR!"), false);
  assert.equal(canConfirmReplace(""), false);
});

test("a data do último backup aparece formatada, e data inválida não quebra", () => {
  assert.match(formatBackupDate("2026-11-26T15:00:00Z"), /2026/);
  assert.equal(formatBackupDate("não é data"), "data inválida");
});

test("as funções puras não usam armazenamento, console nem rede", async () => {
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

  try {
    defaultBackupName(new Date(2026, 10, 26));
    passphraseStrength("Abcdefghijklmnop1");
    validatePassphrase("abcdefghijkl", "abcdefghijkl");
    canConfirmReplace("SUBSTITUIR");
    formatBackupDate("2026-11-26T15:00:00Z");
  } finally {
    g.console = original.console;
    g.fetch = original.fetch;
    g.localStorage = original.ls;
    g.sessionStorage = original.ss;
  }

  assert.deepEqual(vazamentos, []);
});

test("o módulo de backup não toca armazenamento do navegador nem console", () => {
  const src = readFileSync(new URL("../src/lib/backup.ts", import.meta.url), "utf8");
  assert.doesNotMatch(src, /localStorage|sessionStorage|indexedDB|console\.|fetch\(/);
});
