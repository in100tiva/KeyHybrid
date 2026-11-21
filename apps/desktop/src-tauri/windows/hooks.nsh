; O gateway continua rodando depois que a janela fecha e mantém o
; keyhybrid-server.exe travado; sem encerrá-lo, reinstalar ou desinstalar
; falha ao sobrescrever/remover o arquivo.

!macro NSIS_HOOK_PREINSTALL
  nsExec::Exec 'taskkill /F /IM keyhybrid-server.exe'
  Pop $0
  Sleep 500
!macroend

!macro NSIS_HOOK_PREUNINSTALL
  nsExec::Exec 'taskkill /F /IM keyhybrid-server.exe'
  Pop $0
  Sleep 500
!macroend
