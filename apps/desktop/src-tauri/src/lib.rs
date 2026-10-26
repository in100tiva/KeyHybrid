mod gateway;
mod vault;

use std::net::TcpStream;
use std::path::PathBuf;
use std::process::Command;
use std::time::Duration;
use tauri::Manager;

#[cfg(target_os = "windows")]
use std::os::windows::process::CommandExt;

pub fn ensure_gateway_running() {
    let addr = "127.0.0.1:8090".parse().unwrap();
    if TcpStream::connect_timeout(&addr, Duration::from_millis(250)).is_ok() {
        return;
    }

    let bin_name = if cfg!(target_os = "windows") {
        "keyhybrid-server.exe"
    } else {
        "keyhybrid-server"
    };

    let cwd = std::env::current_dir().unwrap_or_default();
    let exe_dir = std::env::current_exe()
        .ok()
        .and_then(|p| p.parent().map(|d| d.to_path_buf()))
        .unwrap_or_default();
    let home_dir = std::env::var("HOME")
        .or_else(|_| std::env::var("USERPROFILE"))
        .map(PathBuf::from)
        .unwrap_or_default();

    let candidates = [
        exe_dir.join(bin_name),
        exe_dir.join("server").join(bin_name),
        cwd.join(bin_name),
        cwd.join("apps").join("server").join(bin_name),
        cwd.join("..").join("server").join(bin_name),
        cwd.join("..").join("..").join("server").join(bin_name),
        home_dir.join(".keyhybrid").join("bin").join(bin_name),
    ];

    for bin_path in candidates {
        if bin_path.exists() {
            if let Some(parent) = bin_path.parent() {
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
                break;
            }
        }
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
        .setup(|app| {
            // Remover sombra/borda nativa da janela para cantos arredondados puros (Windows & Linux)
            if let Some(main_win) = app.get_webview_window("main") {
                let _ = main_win.set_decorations(false);
                let _ = main_win.set_shadow(false);
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
            gateway::delete_vault_key
        ])
        .run(tauri::generate_context!())
        .expect("Erro ao executar aplicação KeyHybrid Tauri");
}
