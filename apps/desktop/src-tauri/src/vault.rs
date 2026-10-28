use argon2::{Algorithm, Argon2, Params, Version};
use keyring::Entry;
use sha2::{Digest, Sha256};
use std::fs;
use std::path::PathBuf;
use std::sync::Mutex;
use std::time::{Duration, Instant};
use zeroize::{Zeroize, ZeroizeOnDrop};

#[derive(Zeroize, ZeroizeOnDrop)]
pub struct SecretHolder {
    pub secret: String,
}

const SERVICE_NAME: &str = "KeyHybridVault";
const PIN_ENTRY_KEY: &str = "__master_pin_6digit__";

struct LockState {
    unlocked_until: Option<Instant>,
}
static LOCK_STATE: Mutex<LockState> = Mutex::new(LockState {
    unlocked_until: None,
});

pub fn is_unlocked() -> bool {
    let state = LOCK_STATE.lock().unwrap_or_else(|e| e.into_inner());
    match state.unlocked_until {
        Some(until) => Instant::now() < until,
        None => false,
    }
}

pub fn touch_unlock() {
    let mut state = LOCK_STATE.lock().unwrap_or_else(|e| e.into_inner());
    state.unlocked_until = Some(Instant::now() + Duration::from_secs(15 * 60));
}

pub fn relock() {
    let mut state = LOCK_STATE.lock().unwrap_or_else(|e| e.into_inner());
    state.unlocked_until = None;
}

pub fn assert_local_origin(window: &tauri::WebviewWindow) -> Result<(), String> {
    let url = window.url().map_err(|e| format!("Erro ao obter URL da janela: {e}"))?;
    let u_str = url.as_str();
    if u_str.starts_with("tauri://localhost")
        || u_str.starts_with("http://tauri.localhost")
        || u_str.starts_with("https://tauri.localhost")
        || u_str.starts_with("http://localhost:1420")
        || u_str.starts_with("http://127.0.0.1:1420")
    {
        Ok(())
    } else {
        Err("Acesso negado: comandos restritos à interface local do KeyHybrid".to_string())
    }
}

fn fallback_pin_path() -> PathBuf {
    let home = std::env::var("HOME")
        .or_else(|_| std::env::var("USERPROFILE"))
        .unwrap_or_else(|_| ".".to_string());
    let dir = PathBuf::from(home).join(".keyhybrid");
    let _ = fs::create_dir_all(&dir);
    dir.join(".pin.vault")
}

fn pin_state_path() -> PathBuf {
    fallback_pin_path().with_extension("state")
}

#[derive(serde::Serialize, serde::Deserialize, Default)]
struct PersistedPinState {
    falhas: u32,
    bloqueado_ate_unix: Option<u64>,
}

fn load_pin_state() -> PersistedPinState {
    if let Ok(data) = fs::read_to_string(pin_state_path()) {
        if let Ok(state) = serde_json::from_str(&data) {
            return state;
        }
    }
    PersistedPinState::default()
}

fn save_pin_state(state: &PersistedPinState) {
    if let Ok(json) = serde_json::to_string(state) {
        let _ = fs::write(pin_state_path(), json);
    }
}

fn current_unix_time() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs()
}

#[allow(dead_code)]
pub fn store_in_dpapi(_alias: &str, _secret: &str) -> Result<(), String> {
    // KH-07: segredos individuais não são gravados em claro nas credenciais do SO
    Ok(())
}

#[allow(dead_code)]
pub fn remove_from_dpapi(alias: &str) {
    if let Ok(entry) = Entry::new(SERVICE_NAME, alias) {
        let _ = entry.delete_credential();
    }
}

fn hash_pin_argon2(salt: &[u8], pin: &str) -> Result<Vec<u8>, String> {
    let params = Params::new(65536, 3, 4, Some(32)).map_err(|e| e.to_string())?;
    let argon2 = Argon2::new(Algorithm::Argon2id, Version::V0x13, params);
    let mut out = [0u8; 32];
    argon2
        .hash_password_into(pin.as_bytes(), salt, &mut out)
        .map_err(|e| e.to_string())?;
    Ok(out.to_vec())
}

fn hash_pin_sha256(salt: &[u8], pin: &str) -> Vec<u8> {
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

/// Formato guardado: "argon2id:<salt_hex>:<hash_hex>"
fn encode_pin(salt: &[u8], pin: &str) -> String {
    match hash_pin_argon2(salt, pin) {
        Ok(h) => format!("argon2id:{}:{}", to_hex(salt), to_hex(&h)),
        Err(_) => format!("sha256:{}:{}", to_hex(salt), to_hex(&hash_pin_sha256(salt, pin))),
    }
}

/// Gera salt aleatório de 16 bytes e devolve o valor a guardar
fn make_stored(pin: &str) -> Result<String, String> {
    let mut salt = [0u8; 16];
    getrandom::getrandom(&mut salt).map_err(|e| e.to_string())?;
    Ok(encode_pin(&salt, pin))
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
    let mut parts = stored.split(':');
    let kind = parts.next()?;
    let salt = from_hex(parts.next()?)?;
    let hash = from_hex(parts.next()?)?;
    if parts.next().is_some() {
        return None;
    }
    match kind {
        "argon2id" => {
            let ok = match hash_pin_argon2(&salt, pin) {
                Ok(expected) => ct_eq(&expected, &hash),
                Err(_) => false,
            };
            Some((ok, false))
        }
        "sha256" => {
            let ok = ct_eq(&hash_pin_sha256(&salt, pin), &hash);
            // Formato sha256 precisa de migração para Argon2id
            Some((ok, true))
        }
        _ => None,
    }
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

fn persist_pin(stored: &str) -> Result<(), String> {
    if let Ok(entry) = Entry::new(SERVICE_NAME, PIN_ENTRY_KEY) {
        let _ = entry.set_password(stored);
    }
    fs::write(fallback_pin_path(), stored)
        .map_err(|e| format!("Falha ao gravar arquivo do PIN: {e}"))?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = fs::set_permissions(fallback_pin_path(), fs::Permissions::from_mode(0o600));
    }
    Ok(())
}

/// Atraso após a 5ª falha seguida: 30 s, dobrando, teto 24 h (KH-08)
fn backoff_secs(falhas: u32) -> u64 {
    (30u64 << falhas.saturating_sub(5).min(12)).min(86400)
}

fn bloqueio_restante() -> Option<u64> {
    let state = load_pin_state();
    let agora = current_unix_time();
    if let Some(ate) = state.bloqueado_ate_unix {
        if agora < ate {
            return Some(ate - agora);
        }
    }
    None
}

fn registrar_resultado(acertou: bool) {
    let mut state = load_pin_state();
    if acertou {
        state.falhas = 0;
        state.bloqueado_ate_unix = None;
    } else {
        state.falhas = state.falhas.saturating_add(1);
        if state.falhas >= 5 {
            state.bloqueado_ate_unix = Some(current_unix_time() + backoff_secs(state.falhas));
        }
    }
    save_pin_state(&state);
}

/// Verifica se existe um PIN de 6 dígitos configurado
#[tauri::command]
pub fn has_vault_pin() -> Result<bool, String> {
    Ok(stored_pin().is_some_and(|s| is_legacy(&s) || s.starts_with("sha256:") || s.starts_with("argon2id:")))
}

/// Define um novo PIN de 6 dígitos
#[tauri::command]
pub fn set_vault_pin(window: tauri::WebviewWindow, pin: String, current_pin: Option<String>) -> Result<(), String> {
    assert_local_origin(&window)?;
    let holder = SecretHolder {
        secret: pin.trim().to_string(),
    };
    if holder.secret.len() != 6 || !holder.secret.chars().all(|c| c.is_ascii_digit()) {
        return Err("O PIN deve conter exatamente 6 dígitos numéricos".to_string());
    }
    if has_vault_pin()? {
        if !is_unlocked() {
            return Err("Destrave o cofre antes de trocar o PIN.".to_string());
        }
        let cur = current_pin.ok_or_else(|| "Informe o PIN atual para alteração.".to_string())?;
        let stored = stored_pin().ok_or_else(|| "Erro ao ler PIN configurado.".to_string())?;
        if check_pin(&stored, cur.trim()).map(|(ok, _)| ok) != Some(true) {
            return Err("PIN atual incorreto.".to_string());
        }
    }

    persist_pin(&make_stored(&holder.secret)?)?;
    relock(); // Exige verificação explícita do PIN recém-definido para desbloquear a sessão
    Ok(())
}

/// Valida o PIN de 6 dígitos com limpeza imediata da memória RAM
#[tauri::command]
pub fn verify_vault_pin(window: tauri::WebviewWindow, pin: String) -> Result<bool, String> {
    assert_local_origin(&window)?;
    if let Some(segundos) = bloqueio_restante() {
        return Err(format!("Muitas tentativas. Aguarde {segundos} s."));
    }
    let Some(stored) = stored_pin().map(|secret| SecretHolder { secret }) else {
        return Err("Nenhum PIN configurado no cofre.".to_string());
    };
    let input_holder = SecretHolder {
        secret: pin.trim().to_string(),
    };
    match check_pin(&stored.secret, &input_holder.secret) {
        Some((true, precisa_migrar)) => {
            if precisa_migrar {
                // Troca o PIN antigo pelo hash Argon2id (KH-08 / KH-18)
                if let Ok(novo) = make_stored(&input_holder.secret) {
                    if let Err(e) = persist_pin(&novo) {
                        eprintln!("[KeyHybrid Vault] Falha ao persistir migração do PIN: {e}");
                    }
                }
            }
            registrar_resultado(true);
            touch_unlock();
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
pub fn clear_vault_pin(window: tauri::WebviewWindow, current_pin: Option<String>) -> Result<(), String> {
    assert_local_origin(&window)?;
    if !is_unlocked() {
        return Err("Destrave o cofre antes de remover o PIN.".to_string());
    }
    let cur = current_pin.ok_or_else(|| "Informe o PIN atual para remoção.".to_string())?;
    let stored = stored_pin().ok_or_else(|| "Nenhum PIN configurado.".to_string())?;
    if check_pin(&stored, cur.trim()).map(|(ok, _)| ok) != Some(true) {
        return Err("PIN atual incorreto.".to_string());
    }
    if let Ok(entry) = Entry::new(SERVICE_NAME, PIN_ENTRY_KEY) {
        let _ = entry.delete_credential();
    }
    let _ = fs::remove_file(fallback_pin_path());
    let _ = fs::remove_file(pin_state_path());
    relock();
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    const SALT: [u8; 16] = [7; 16];

    #[test]
    fn pin_novo_bate_so_com_o_certo() {
        let stored = encode_pin(&SALT, "123456");
        assert!(stored.starts_with("argon2id:"));
        assert!(!stored.contains("123456"));
        assert_eq!(check_pin(&stored, "123456"), Some((true, false)));
        assert_eq!(check_pin(&stored, "654321"), Some((false, false)));
    }

    #[test]
    fn pin_antigo_em_claro_bate_e_pede_migracao() {
        assert_eq!(check_pin("123456", "123456"), Some((true, true)));
        assert_eq!(check_pin("123456", "654321"), Some((false, true)));
    }

    #[test]
    fn pin_antigo_sha256_bate_e_pede_migracao() {
        let stored = format!("sha256:{}:{}", to_hex(&SALT), to_hex(&hash_pin_sha256(&SALT, "123456")));
        assert_eq!(check_pin(&stored, "123456"), Some((true, true)));
        assert_eq!(check_pin(&stored, "654321"), Some((false, true)));
    }

    #[test]
    fn formato_desconhecido_nao_bate() {
        assert_eq!(check_pin("md5:abc", "123456"), None);
        assert_eq!(check_pin("", "123456"), None);
    }

    #[test]
    fn atraso_dobra_ate_o_teto() {
        assert_eq!(backoff_secs(4), 30);
        assert_eq!(backoff_secs(5), 30);
        assert_eq!(backoff_secs(6), 60);
        assert_eq!(backoff_secs(7), 120);
        assert_eq!(backoff_secs(8), 240);
        assert_eq!(backoff_secs(16), 61440);
        assert_eq!(backoff_secs(20), 86400);
        assert_eq!(backoff_secs(30), 86400); // Teto 24h
    }

    #[test]
    fn kh06_lock_estado_e_expiracao() {
        relock();
        assert!(!is_unlocked());
        touch_unlock();
        assert!(is_unlocked());
        relock();
        assert!(!is_unlocked());
    }
}
