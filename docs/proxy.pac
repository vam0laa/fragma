// Send only the blocked sites through vamola-dpi, everything else goes direct.
// Firefox: Settings > Network Settings > "Automatic proxy configuration URL"
//          file:///path/to/proxy.pac
function FindProxyForURL(url, host) {
  var blocked = ["discord.com", "discord.gg", "discordapp.com", "discordapp.net", "discord.media"];
  for (var i = 0; i < blocked.length; i++) {
    if (host === blocked[i] || dnsDomainIs(host, "." + blocked[i])) {
      return "PROXY 127.0.0.1:8899";
    }
  }
  return "DIRECT";
}
