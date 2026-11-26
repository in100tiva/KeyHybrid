// Backup do cofre (#26). Roda com `pnpm run test`: node --test, sem dependência nova.
// Senhas são falsas e só servem para medir tamanho e confirmação.
// @ts-expect-error sem @types/node, como no envImport.test.ts
import { test } from "node:test";
// @ts-expect-error sem @types/node
import assert from "node:assert/strict";
// @ts-expect-error sem @types/node
import { readFileSync } from "node:fs";
import {
  backupReminder,
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

test("lembrete de backup: nunca e 7+ dias pedem destaque; 0 a 6 dias não", () => {
  const agora = new Date("2026-11-26T12:00:00Z");
  const ha = (dias: number, horas = 0) => new Date(agora.getTime() - (dias * 24 + horas) * 3600_000).toISOString();
  assert.deepEqual(backupReminder(null, agora), { text: "Nenhum backup .khvault ainda.", alert: true });
  assert.deepEqual(backupReminder(undefined, agora), { text: "Nenhum backup .khvault ainda.", alert: true });
  assert.deepEqual(backupReminder("não é data", agora), { text: "Nenhum backup .khvault ainda.", alert: true });
  assert.deepEqual(backupReminder(ha(0), agora), { text: "Último backup .khvault: hoje.", alert: false });
  assert.deepEqual(backupReminder(ha(1), agora), { text: "Último backup .khvault: há 1 dia.", alert: false });
  // 6 dias e 23 h ainda são 6 dias completos
  assert.deepEqual(backupReminder(ha(6, 23), agora), { text: "Último backup .khvault: há 6 dias.", alert: false });
  assert.deepEqual(backupReminder(ha(7), agora), { text: "Último backup .khvault: há 7 dias. Faça um novo.", alert: true });
  assert.equal(backupReminder(ha(30), agora).alert, true);
  // Relógio adiantado: backup "no futuro" conta como hoje
  assert.deepEqual(backupReminder(ha(-2), agora), { text: "Último backup .khvault: hoje.", alert: false });
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
    backupReminder("2026-11-26T15:00:00Z", new Date());
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
