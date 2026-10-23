using System.Text.Json;
using System.Text.Json.Serialization;

namespace JukuWinUI.Core;

public sealed class AppConfig
{
    public string ServerUrl { get; set; } = "";
    public string Username { get; set; } = "";
    public string Password { get; set; } = "";
    public string Theme { get; set; } = "light";
    public string Sort { get; set; } = "random";
    public bool HideVip { get; set; }
    public bool AutoNext { get; set; } = true;
    public double PlaybackRate { get; set; } = 1;
    public bool ShowHuangguo { get; set; }
    public bool ShowHuangdou { get; set; }
    public string SelectedSource { get; set; } = "hongguo";

    [JsonIgnore]
    public bool HasCredentials =>
        !string.IsNullOrWhiteSpace(ServerUrl) &&
        !string.IsNullOrWhiteSpace(Username) &&
        !string.IsNullOrWhiteSpace(Password);

    private static string ConfigPath
    {
        get
        {
            var dir = Path.Combine(
                Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData),
                "JukuDesktop");
            Directory.CreateDirectory(dir);
            return Path.Combine(dir, "config.json");
        }
    }

    public static AppConfig Load()
    {
        try
        {
            if (File.Exists(ConfigPath))
            {
                var json = File.ReadAllText(ConfigPath);
                return JsonSerializer.Deserialize<AppConfig>(json) ?? new AppConfig();
            }
        }
        catch { /* ignore */ }
        return new AppConfig();
    }

    public void Save()
    {
        var json = JsonSerializer.Serialize(this, new JsonSerializerOptions { WriteIndented = true });
        File.WriteAllText(ConfigPath, json);
    }

    public static string NormalizeServerUrl(string? url)
    {
        var u = (url ?? "").Trim();
        if (string.IsNullOrEmpty(u)) return "";
        if (!u.StartsWith("http://", StringComparison.OrdinalIgnoreCase) &&
            !u.StartsWith("https://", StringComparison.OrdinalIgnoreCase))
            u = "http://" + u;
        return u.TrimEnd('/');
    }

    public static string SourceGroup(string? source)
    {
        var s = (source ?? "").ToLowerInvariant();
        if (s.StartsWith("hongguo")) return "hongguo";
        if (s.StartsWith("huangguo") || s == "cloudfront") return "huangguo";
        if (s.StartsWith("huangdou")) return "huangdou";
        return s;
    }

    public bool IsSourceHidden(string? source)
    {
        var g = SourceGroup(source);
        if (g == "huangguo") return !ShowHuangguo;
        if (g == "huangdou") return !ShowHuangdou;
        return false;
    }
}
