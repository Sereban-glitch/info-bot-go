// Telegram's supported in-app navigation; the anchor remains the web fallback.
// No sendData, command payload, or selected-request resume is implied.
function szOpenBotChat(event) {
  if (event.defaultPrevented || event.button > 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
  const app = typeof window !== 'undefined' ? window.Telegram?.WebApp : null;
  if (!app?.initData || typeof app.openTelegramLink !== 'function') return;
  try {
    app.openTelegramLink('https://t.me/Infozaputbot');
    event.preventDefault();
    // openTelegramLink no longer closes Mini Apps on Telegram 7.0+. Return to
    // the chat explicitly after successful navigation; never send a command.
    try { if (typeof app.close === 'function') app.close(); } catch {}
  } catch {
    // A failed/unsupported SDK call must not suppress the ordinary HTTPS link.
  }
}
