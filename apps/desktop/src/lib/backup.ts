// Backup do cofre (#26): regras puras da etapa de backup e restauração. Sem Svelte nem Tauri,
// para rodar em `node --test`. A cifra e a leitura do arquivo ficam no Rust (backup.rs).

const MIN_PASSPHRASE = 12;
const LABELS = ["Curta demais", "Fraca", "Razoável", "Boa", "Forte"];

export interface BackupResult {
  path: string;
  items: number;
  created_at: string;
}

export interface BackupPreview {
  created_at: string;
  app_version: string;
  total: number;
  ai_keys: number;
  service_keys: number;
  providers: string[];
  aliases: string[];
  conflicts: string[];
}

export interface RestoreResult {
  mode: "merge" | "replace";
  added: string[];
  conflicts: string[];
  removed: number;
  total_after: number;
  safety_backup: string | null;
}

export interface BackupStatus {
  last_backup: { at: string; path: string; items: number } | null;
  last_restore: { at: string; path: string; items: number; mode: "merge" | "replace" } | null;
}

// Nome sugerido no diálogo de salvar, no horário local: keyhybrid-backup-AAAAMMDD-HHMMSS.khvault
export function defaultBackupName(d: Date): string {
  const p = (n: number) => String(n).padStart(2, "0");
  const data = `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}`;
  const hora = `${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}`;
  return `keyhybrid-backup-${data}-${hora}.khvault`;
}

// Medidor de força: comprimento (caracteres Unicode) + variedade de classes. Abaixo de 12 é recusada.
export function passphraseStrength(p: string): { score: number; label: string } {
  const len = [...p].length;
  if (len < MIN_PASSPHRASE) return { score: 0, label: LABELS[0] };
  const classes = [/\p{Ll}/u, /\p{Lu}/u, /\p{Nd}/u, /[^\p{L}\p{Nd}]/u].filter((re) => re.test(p)).length;
  const score = Math.min(4, Math.min(classes, 2) + (len >= 16 ? 1 : 0) + (len >= 20 ? 1 : 0));
  return { score, label: LABELS[score] };
}

// Mensagem de erro para mostrar na tela, ou null quando a senha pode ser usada
export function validatePassphrase(p: string, confirmacao: string): string | null {
  if ([...p].length < MIN_PASSPHRASE) return "A senha do backup precisa de pelo menos 12 caracteres.";
  if (p !== confirmacao) return "As duas senhas não são iguais.";
  return null;
}

// Restaurar substituindo tudo exige a palavra exata (espaços das pontas não contam)
export function canConfirmReplace(texto: string): boolean {
  return texto.trim() === "SUBSTITUIR";
}

export function formatBackupDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "data inválida";
  return d.toLocaleString("pt-BR", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

const DIA_MS = 24 * 60 * 60 * 1000;
const ALERTA_DIAS = 7;

// Lembrete da tela principal (#53): nunca ou 7+ dias pede destaque, e o texto diz o motivo (não só a cor).
// Data inválida conta como "nunca"; data no futuro (relógio adiantado) conta como hoje.
export function backupReminder(lastAt: string | null | undefined, now: Date): { text: string; alert: boolean } {
  const t = lastAt ? new Date(lastAt).getTime() : NaN;
  if (Number.isNaN(t)) return { text: "Nenhum backup .khvault ainda.", alert: true };
  const dias = Math.max(0, Math.floor((now.getTime() - t) / DIA_MS));
  const quando = dias === 0 ? "hoje" : dias === 1 ? "há 1 dia" : `há ${dias} dias`;
  const alert = dias >= ALERTA_DIAS;
  return { text: `Último backup .khvault: ${quando}.${alert ? " Faça um novo." : ""}`, alert };
}
