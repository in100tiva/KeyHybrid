// Último uso por alias (#55): texto puro, sem Svelte nem Tauri, para rodar em `node --test`.

export interface KeyUsage {
  last_used_at?: string | null;
  last_status?: number | null;
}

const rtf = new Intl.RelativeTimeFormat("pt-BR", { numeric: "auto" });

function relative(iso: string, now: number): string {
  const secs = Math.round((Date.parse(iso) - now) / 1000);
  if (Number.isNaN(secs)) return "";
  const abs = Math.abs(secs);
  if (abs < 60) return rtf.format(secs, "second");
  if (abs < 3600) return rtf.format(Math.round(secs / 60), "minute");
  if (abs < 86400) return rtf.format(Math.round(secs / 3600), "hour");
  return rtf.format(Math.round(secs / 86400), "day");
}

/** "usado há 5 minutos · HTTP 200"; vazio quando o gateway ainda não viu uso do alias. */
export function usageLabel(k: KeyUsage, now = Date.now()): string {
  if (!k.last_used_at || !k.last_status) return "";
  const when = relative(k.last_used_at, now);
  return when ? `usado ${when} · HTTP ${k.last_status}` : `HTTP ${k.last_status}`;
}

/** 401/403 do provedor: a chave expirou ou foi revogada. */
export function usageRefused(k: KeyUsage): boolean {
  return k.last_status === 401 || k.last_status === 403;
}
