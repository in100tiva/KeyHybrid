use keyring::Entry;
use sha2::{Digest, Sha256};
use std::fs;
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Mutex;
use std::time::{Duration, Instant};
use zeroize::{Zeroize, ZeroizeOnDrop};

#[derive(Zeroize, ZeroizeOnDrop)]
pub struct SecretHolder {
    pub secret: String,
}

const SERVICE_NAME: &str = "KeyHybridVault";
const PIN_ENTRY_KEY: &str = "__master_pin_6digit__";

// Tentativas falhas seguidas e bloqueio ativo (só em memória: reiniciar o app zera)
struct Tentativas {
    falhas: u32,
    bloqueado_ate: Option<Instant>,
}
static TENTATIVAS: Mutex<Tentativas> = Mutex::new(Tentativas {
    falhas: 0,
    bloqueado_ate: None,
});
// Cofre destravado nesta sessão (PIN conferido ou definido)
static UNLOCKED: AtomicBool = AtomicBool::new(false);

fn fallback_pin_path() -> PathBuf {
    let home = std::env::var("HOME")
        .or_else(|_| std::env::var("USERPROFILE"))
        .unwrap_or_else(|_| ".".to_string());
    let dir = PathBuf::from(home).join(".keyhybrid");
    let _ = fs::create_dir_all(&dir);
    dir.join(".pin.vault")
}

pub fn store_in_dpapi(alias: &str, secret: &str) -> Result<(), String> {
    if let Ok(entry) = Entry::new(SERVICE_NAME, alias) {
        let _ = entry.set_password(secret);
    }
    Ok(())
}

pub fn remove_from_dpapi(alias: &str) {
    if let Ok(entry) = Entry::new(SERVICE_NAME, alias) {
        let _ = entry.delete_credential();
    }
}

// shortcut: PIN de 6 dígitos com SHA-256 cai em força bruta offline; o que segura é o atraso entre tentativas. Upgrade: Argon2 se o PIN virar senha.
fn hash_pin(salt: &[u8], pin: &str) -> Vec<u8> {
    Sha256::new()
        .chain_update(salt)
        .chain_update(pin.as_bytes())
        .finalize()
        .to_vec()
}

fn to_hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

fn from_hex(s: &str) -> Option<Vec<u8>> {
    if s.len() % 2 != 0 {
        return None;
    }
    (0..s.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(s.get(i..i + 2)?, 16).ok())
        .collect()
}

/// Formato guardado: "sha256:<salt_hex>:<hash_hex>"
fn encode_pin(salt: &[u8], pin: &str) -> String {
    format!("sha256:{}:{}", to_hex(salt), to_hex(&hash_pin(salt, pin)))
}

/// Gera salt aleatório de 16 bytes e devolve o valor a guardar
fn make_stored(pin: &str) -> Result<String, String> {
    let mut salt = [0u8; 16];
    getrandom::getrandom(&mut salt).map_err(|e| e.to_string())?;
    Ok(encode_pin(&salt, pin))
}

fn parse_hashed(stored: &str) -> Option<(Vec<u8>, Vec<u8>)> {
    let mut parts = stored.split(':');
    if parts.next()? != "sha256" {
        return None;
    }
    let salt = from_hex(parts.next()?)?;
    let hash = from_hex(parts.next()?)?;
    parts.next().is_none().then_some((salt, hash))
}

/// Formato antigo: PIN de 6 dígitos em claro
fn is_legacy(stored: &str) -> bool {
    stored.len() == 6 && stored.bytes().all(|b| b.is_ascii_digit())
}

fn ct_eq(a: &[u8], b: &[u8]) -> bool {
    a.len() == b.len() && a.iter().zip(b).fold(0u8, |acc, (x, y)| acc | (x ^ y)) == 0
}

/// Some((acertou, precisa_migrar)); None se o formato guardado for desconhecido
fn check_pin(stored: &str, pin: &str) -> Option<(bool, bool)> {
    if is_legacy(stored) {
        return Some((ct_eq(stored.as_bytes(), pin.as_bytes()), true));
    }
    let (salt, hash) = parse_hashed(stored)?;
    Some((ct_eq(&hash_pin(&salt, pin), &hash), false))
}

/// Lê o PIN guardado: keyring primeiro, depois o arquivo de fallback
fn stored_pin() -> Option<String> {
    if let Ok(entry) = Entry::new(SERVICE_NAME, PIN_ENTRY_KEY) {
        if let Ok(value) = entry.get_password() {
            return Some(value.trim().to_string());
        }
    }
    fs::read_to_string(fallback_pin_path())
        .ok()
        .map(|s| s.trim().to_string())
}

fn persist_pin(stored: &str) {
    if let Ok(entry) = Entry::new(SERVICE_NAME, PIN_ENTRY_KEY) {
        let _ = entry.set_password(stored);
    }
    let _ = fs::write(fallback_pin_path(), stored);
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = fs::set_permissions(fallback_pin_path(), fs::Permissions::from_mode(0o600));
    }
}

/// Atraso após a 5ª falha seguida: 30 s, dobrando, teto 15 min
fn backoff_secs(falhas: u32) -> u64 {
    (30u64 << falhas.saturating_sub(5).min(5)).min(900)
}

fn bloqueio_restante() -> Option<u64> {
    let t = TENTATIVAS.lock().unwrap_or_else(|e| e.into_inner());
    let ate = t.bloqueado_ate?;
    let agora = Instant::now();
    (agora < ate).then(|| (ate - agora).as_secs_f64().ceil() as u64)
}

fn registrar_resultado(acertou: bool) {
    let mut t = TENTATIVAS.lock().unwrap_or_else(|e| e.into_inner());
    if acertou {
        t.falhas = 0;
        t.bloqueado_ate = None;
    } else {
        t.falhas = t.falhas.saturating_add(1);
        if t.falhas >= 5 {
            t.bloqueado_ate = Some(Instant::now() + Duration::from_secs(backoff_secs(t.falhas)));
        }
    }
}

/// Verifica se existe um PIN de 6 dígitos configurado (Windows DPAPI / Linux Secret Service + Fallback local)
#[tauri::command]
pub fn has_vault_pin() -> Result<bool, String> {
    Ok(stored_pin().is_some_and(|s| is_legacy(&s) || parse_hashed(&s).is_some()))
}

/// Define um novo PIN de 6 dígitos (Windows DPAPI / Linux Secret Service + Fallback local)
#[tauri::command]
pub fn set_vault_pin(pin: String) -> Result<(), String> {
    let holder = SecretHolder {
        secret: pin.trim().to_string(),
    };
    if holder.secret.len() != 6 || !holder.secret.chars().all(|c| c.is_ascii_digit()) {
        return Err("O PIN deve conter exatamente 6 dígitos numéricos".to_string());
    }
    // Trocar um PIN existente exige o cofre destravado, senão qualquer chamada redefinia o PIN
    if has_vault_pin()? && !UNLOCKED.load(Ordering::SeqCst) {
        return Err("Destrave o cofre antes de trocar o PIN.".to_string());
    }

    persist_pin(&make_stored(&holder.secret)?);
    UNLOCKED.store(true, Ordering::SeqCst);
    Ok(())
}

/// Valida o PIN de 6 dígitos com limpeza imediata da memória RAM
#[tauri::command]
pub fn verify_vault_pin(pin: String) -> Result<bool, String> {
    if let Some(segundos) = bloqueio_restante() {
        return Err(format!("Muitas tentativas. Aguarde {segundos} s."));
    }
    let Some(stored) = stored_pin().map(|secret| SecretHolder { secret }) else {
        return Ok(true);
    };
    let input_holder = SecretHolder {
        secret: pin.trim().to_string(),
    };
    match check_pin(&stored.secret, &input_holder.secret) {
        Some((true, precisa_migrar)) => {
            if precisa_migrar {
                // Troca o PIN em claro pelo hash; falha aqui não bloqueia o acerto
                if let Ok(novo) = make_stored(&input_holder.secret) {
                    persist_pin(&novo);
                }
            }
            registrar_resultado(true);
            UNLOCKED.store(true, Ordering::SeqCst);
            Ok(true)
        }
        _ => {
            registrar_resultado(false);
            Ok(false)
        }
    }
}

/// Remove o PIN de bloqueio do cofre (só com o cofre destravado nesta sessão)
#[tauri::command]
pub fn clear_vault_pin() -> Result<(), String> {
    if !UNLOCKED.load(Ordering::SeqCst) {
        return Err("Destrave o cofre antes de remover o PIN.".to_string());
    }
    if let Ok(entry) = Entry::new(SERVICE_NAME, PIN_ENTRY_KEY) {
        let _ = entry.delete_credential();
    }
    let _ = fs::remove_file(fallback_pin_path());
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    const SALT: [u8; 16] = [7; 16];

    #[test]
    fn pin_novo_bate_so_com_o_certo() {
        let stored = encode_pin(&SALT, "123456");
        assert!(stored.starts_with("sha256:"));
        assert!(!stored.contains("123456"));
        assert_eq!(check_pin(&stored, "123456"), Some((true, false)));
        assert_eq!(check_pin(&stored, "654321"), Some((false, false)));
    }

    #[test]
    fn pin_antigo_em_claro_bate_e_pede_migracao() {
        assert!(is_legacy("123456"));
        assert_eq!(check_pin("123456", "123456"), Some((true, true)));
        assert_eq!(check_pin("123456", "000000"), Some((false, true)));
        assert!(!is_legacy(&encode_pin(&SALT, "123456")));
    }

    #[test]
    fn formato_desconhecido_nao_bate() {
        assert_eq!(check_pin("lixo", "123456"), None);
        assert_eq!(parse_hashed("sha256:zz:00"), None);
    }

    #[test]
    fn atraso_dobra_ate_o_teto_de_15_min() {
        assert_eq!(backoff_secs(5), 30);
        assert_eq!(backoff_secs(6), 60);
        assert_eq!(backoff_secs(9), 480);
        assert_eq!(backoff_secs(12), 900);
    }
}
