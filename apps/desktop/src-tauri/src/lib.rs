mod backup;
mod gateway;
mod server_bin;
mod vault;

use std::net::TcpStream;
use std::path::PathBuf;
use std::process::Command;
use std::sync::OnceLock;
use std::time::Duration;
use tauri::Manager;

#[cfg(target_os = "windows")]
use std::os::windows::process::CommandExt;

/// Diretório de recursos empacotados do instalador.
static RESOURCE_DIR: OnceLock<PathBuf> = OnceLock::new();

/// Localiza o binário do gateway Go (ao lado do app, em recursos ou no diretório do projeto).
pub(crate) fn find_server_bin() -> Option<PathBuf> {
    let bin_name = server_bin::bin_name();
    let cwd = std::env::current_dir().unwrap_or_default();
    let exe_dir = std::env::current_exe()
        .ok()
        .and_then(|p| p.parent().map(|d| d.to_path_buf()))
        .unwrap_or_default();
    let home_dir = std::env::var("HOME")
        .or_else(|_| std::env::var("USERPROFILE"))
        .map(PathBuf::from)
        .unwrap_or_default();

    let candidates = server_bin::candidates(
        bin_name,
        &exe_dir,
        &cwd,
        &home_dir,
        RESOURCE_DIR.get().map(PathBuf::as_path),
    );

    server_bin::first_existing(&candidates)
}

/// Pasta das unidades do systemd do usuário: $XDG_CONFIG_HOME/systemd/user ou ~/.config/systemd/user
#[cfg(target_os = "linux")]
fn systemd_user_dir() -> PathBuf {
    std::env::var_os("XDG_CONFIG_HOME")
        .filter(|v| !v.is_empty())
        .map(PathBuf::from)
        .or_else(|| std::env::var_os("HOME").map(|h| PathBuf::from(h).join(".config")))
        .unwrap_or_default()
        .join("systemd")
        .join("user")
}

/// `systemctl --user enable keyhybrid.service` cria este link em default.target.wants
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
fn servico_systemd_habilitado(systemd_user_dir: &std::path::Path) -> bool {
    systemd_user_dir
        .join("default.target.wants")
        .join("keyhybrid.service")
        .exists()
}

pub fn ensure_gateway_running() {
    let addr = "127.0.0.1:8090".parse().unwrap();
    if TcpStream::connect_timeout(&addr, Duration::from_millis(250)).is_ok() {
        return;
    }

    // Gateway gerenciado pelo systemd (install_linux.sh): pede ao serviço em vez de subir um
    // processo próprio, que segurava a porta com o binário antigo durante atualizações (#37)
    #[cfg(target_os = "linux")]
    if servico_systemd_habilitado(&systemd_user_dir()) {
        let _ = Command::new("systemctl")
            .args(["--user", "start", "keyhybrid.service"])
            .status();
        return;
    }

    if let Some(bin_path) = find_server_bin()
        && let Some(parent) = bin_path.parent()
    {
        let mut cmd = Command::new(&bin_path);
        cmd.arg("serve")
            .arg("--http=127.0.0.1:8090")
            .current_dir(parent);
        #[cfg(target_os = "windows")]
        {
            const CREATE_NO_WINDOW: u32 = 0x08000000;
            cmd.creation_flags(CREATE_NO_WINDOW);
        }
        let _ = cmd.spawn();
    }
}

#[tauri::command]
fn window_minimize(window: tauri::Window) {
    let _ = window.minimize();
}

#[tauri::command]
fn window_close(window: tauri::Window) {
    let _ = window.close();
}

#[tauri::command]
fn window_start_dragging(window: tauri::Window) {
    let _ = window.start_dragging();
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_dialog::init())
        .setup(|app| {
            // Remover sombra/borda nativa da janela para cantos arredondados puros (Windows & Linux)
            if let Some(main_win) = app.get_webview_window("main") {
                let _ = main_win.set_decorations(false);
                let _ = main_win.set_shadow(false);
            }

            if let Ok(dir) = app.path().resource_dir() {
                let _ = RESOURCE_DIR.set(dir);
            }

            // Garantir que o servidor Gateway em Go esteja rodando automaticamente
            std::thread::spawn(|| {
                ensure_gateway_running();
            });

            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            window_minimize,
            window_close,
            window_start_dragging,
            vault::has_vault_pin,
            vault::set_vault_pin,
            vault::verify_vault_pin,
            vault::clear_vault_pin,
            gateway::check_gateway_health,
            gateway::list_vault_keys,
            gateway::save_vault_key,
            gateway::activate_vault_key,
            gateway::delete_vault_key,
            backup::choose_backup_destination,
            backup::choose_backup_file,
            backup::backup_vault,
            backup::inspect_backup,
            backup::restore_vault,
            backup::backup_status
        ])
        .run(tauri::generate_context!())
        .expect("Erro ao executar aplicação KeyHybrid Tauri");
}

#[cfg(test)]
mod tests {
    use super::servico_systemd_habilitado;
    use std::fs;

    #[test]
    fn detecta_servico_systemd_habilitado() {
        let mut r = [0u8; 8];
        getrandom::fill(&mut r).unwrap();
        let dir = std::env::temp_dir().join(format!("kh-systemd-{:x}", u64::from_le_bytes(r)));
        assert!(!servico_systemd_habilitado(&dir));
        let wants = dir.join("default.target.wants");
        fs::create_dir_all(&wants).unwrap();
        assert!(!servico_systemd_habilitado(&dir)); // só a pasta, sem o serviço
        fs::write(wants.join("keyhybrid.service"), "").unwrap();
        assert!(servico_systemd_habilitado(&dir));
        let _ = fs::remove_dir_all(&dir);
    }
}
