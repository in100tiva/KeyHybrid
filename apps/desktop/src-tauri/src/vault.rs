use keyring::Entry;
use std::fs;
use std::path::PathBuf;
use zeroize::{Zeroize, ZeroizeOnDrop};

#[derive(Zeroize, ZeroizeOnDrop)]
pub struct SecretHolder {
    pub secret: String,
}

const SERVICE_NAME: &str = "KeyHybridVault";
const PIN_ENTRY_KEY: &str = "__master_pin_6digit__";

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

/// Verifica se existe um PIN de 6 dígitos configurado (Windows DPAPI / Linux Secret Service + Fallback local)
#[tauri::command]
pub fn has_vault_pin() -> Result<bool, String> {
    if let Ok(entry) = Entry::new(SERVICE_NAME, PIN_ENTRY_KEY) {
        if let Ok(pin) = entry.get_password() {
            let holder = SecretHolder { secret: pin };
            if holder.secret.len() == 6 {
                return Ok(true);
            }
        }
    }
    if let Ok(pin) = fs::read_to_string(fallback_pin_path()) {
        let holder = SecretHolder {
            secret: pin.trim().to_string(),
        };
        return Ok(holder.secret.len() == 6);
    }
    Ok(false)
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

    let mut saved_in_keyring = false;
    if let Ok(entry) = Entry::new(SERVICE_NAME, PIN_ENTRY_KEY) {
        if entry.set_password(&holder.secret).is_ok() {
            saved_in_keyring = true;
        }
    }
    let _ = fs::write(fallback_pin_path(), &holder.secret);
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = fs::set_permissions(fallback_pin_path(), fs::Permissions::from_mode(0o600));
    }
    let _ = saved_in_keyring;
    Ok(())
}

/// Valida o PIN de 6 dígitos com limpeza imediata da memória RAM
#[tauri::command]
pub fn verify_vault_pin(pin: String) -> Result<bool, String> {
    let input_holder = SecretHolder {
        secret: pin.trim().to_string(),
    };
    if let Ok(entry) = Entry::new(SERVICE_NAME, PIN_ENTRY_KEY) {
        if let Ok(stored) = entry.get_password() {
            let stored_holder = SecretHolder { secret: stored };
            return Ok(stored_holder.secret == input_holder.secret);
        }
    }
    if let Ok(stored) = fs::read_to_string(fallback_pin_path()) {
        let stored_holder = SecretHolder {
            secret: stored.trim().to_string(),
        };
        return Ok(stored_holder.secret == input_holder.secret);
    }
    Ok(true)
}

/// Remove o PIN de bloqueio do cofre
#[tauri::command]
pub fn clear_vault_pin() -> Result<(), String> {
    if let Ok(entry) = Entry::new(SERVICE_NAME, PIN_ENTRY_KEY) {
        let _ = entry.delete_credential();
    }
    let _ = fs::remove_file(fallback_pin_path());
    Ok(())
}
