using System.Net;
using System.Text;
using System.Text.Json;

namespace JukuWinUI.Core;

public sealed class JukuApi
{
    private static void LogX(string m)
    {
        try { System.IO.File.AppendAllText(System.IO.Path.Combine(AppContext.BaseDirectory, "juku-err.log"), "[api] " + m + Environment.NewLine); } catch {}
    }
    private static readonly JsonSerializerOptions JsonOpts = new()
    {
        PropertyNameCaseInsensitive = true,
    };

    private readonly HttpClient _http;
    private readonly CookieContainer _cookies = new();
    private string _baseUrl = "";
    private string _viewerId = "";

    public string BaseUrl => _baseUrl;
    public string ViewerId => _viewerId;

    public JukuApi()
    {
        var handler = new HttpClientHandler
        {
            CookieContainer = _cookies,
            UseCookies = true,
            AutomaticDecompression = DecompressionMethods.All,
        };
        _http = new HttpClient(handler)
        {
            Timeout = TimeSpan.FromSeconds(12),
        };
        _http.DefaultRequestHeaders.TryAddWithoutValidation("Sec-Fetch-Site", "none");
        _http.DefaultRequestHeaders.TryAddWithoutValidation("Accept", "application/json, text/plain, */*");
    }

    public void SetBaseUrl(string url) => _baseUrl = (url ?? "").TrimEnd('/');

    public void ResetSession()
    {
        _viewerId = "";
        _cookies.Add(new Uri("http://localhost"), new Cookie("dummy", "1")); // force container
    }

    private HttpRequestMessage Build(string method, string path, object? body, bool skipViewer = false)
    {
        var url = path.StartsWith("http", StringComparison.OrdinalIgnoreCase)
            ? path
            : _baseUrl + path;
        var req = new HttpRequestMessage(new HttpMethod(method), url);
        if (!skipViewer && !string.IsNullOrEmpty(_viewerId))
            req.Headers.TryAddWithoutValidation("X-Juku-Viewer", _viewerId);
        if (body != null)
        {
            var json = JsonSerializer.Serialize(body);
            req.Content = new StringContent(json, Encoding.UTF8, "application/json");
        }
        return req;
    }

    private async Task<T> SendAsync<T>(string method, string path, object? body = null, bool skipViewer = false)
    {
        LogX(method + " " + path);
        using var req = Build(method, path, body, skipViewer);
        using var resp = await _http.SendAsync(req);
        var text = await resp.Content.ReadAsStringAsync();
        if (!resp.IsSuccessStatusCode)
        {
            string msg;
            try
            {
                using var doc = JsonDocument.Parse(string.IsNullOrWhiteSpace(text) ? "{}" : text);
                msg = doc.RootElement.TryGetProperty("error", out var e) ? e.GetString() ?? "" :
                      doc.RootElement.TryGetProperty("message", out var m) ? m.GetString() ?? "" : "";
            }
            catch { msg = ""; }
            if (string.IsNullOrWhiteSpace(msg)) msg = $"请求失败 ({(int)resp.StatusCode})";
            throw new Exception(msg);
        }
        if (typeof(T) == typeof(string))
            return (T)(object)text;
        return JsonSerializer.Deserialize<T>(text, JsonOpts)
               ?? throw new Exception("响应解析失败");
    }

    public async Task<Viewer> FetchViewer(bool confirm = false)
    {
        var path = confirm ? "/api/ui/viewer?confirm=1" : "/api/ui/viewer";
        return await SendAsync<Viewer>("GET", path, null, skipViewer: true);
    }

    public async Task<Viewer> EnsureViewer()
    {
        var first = await FetchViewer(false);
        if (!string.IsNullOrEmpty(first.Id)) _viewerId = first.Id;
        var ready = await FetchViewer(true);
        if (!string.IsNullOrEmpty(ready.Id)) _viewerId = ready.Id;
        return ready;
    }

    public async Task<AccountInfo> Login(string username, string password)
    {
        await EnsureViewer();
        var res = await SendAsync<LoginResponse>("POST", "/api/ui/account/login",
            new { username, password });
        return res.Account ?? new AccountInfo { Username = username };
    }

    public async Task Logout()
    {
        await SendAsync<JsonElement>("POST", "/api/ui/account/logout", new { });
    }

    public async Task<DramasResponse> FetchDramas(bool refresh = false)
    {
        var path = refresh ? "/api/ui/dramas?refresh=1" : "/api/ui/dramas";
        LogX("FetchDramas cut");
        using var req = Build("GET", path, null, false);
        using var resp = await _http.SendAsync(req, System.Net.Http.HttpCompletionOption.ResponseHeadersRead);
        if (!resp.IsSuccessStatusCode) throw new Exception("HTTP " + (int)resp.StatusCode);
        await using var stream = await resp.Content.ReadAsStreamAsync();
        var list = new List<Drama>();
        var acc = new List<byte>();
        var buf = new byte[8 * 1024];
        while (list.Count < 80)
        {
            int n = await stream.ReadAsync(buf.AsMemory(0, buf.Length));
            if (n == 0) break;
            for (int i = 0; i < n; i++) acc.Add(buf[i]);
            foreach (var d in ParseDramasChunk(acc))
                if (list.Count < 80) list.Add(d);
            if (acc.Count > 256 * 1024) acc = acc.GetRange(acc.Count - 32 * 1024, 32 * 1024);
        }
        LogX("parsed " + list.Count);
        return new DramasResponse { Data = list, Total = list.Count };
    }

    private static List<Drama> ParseDramasChunk(List<byte> acc)
    {
        var outList = new List<Drama>();
        try
        {
            var bytes = acc.ToArray();
            var rdr = new System.Text.Json.Utf8JsonReader(bytes);
            while (rdr.Read())
            {
                if (rdr.TokenType == System.Text.Json.JsonTokenType.PropertyName && rdr.ValueTextEquals("data"))
                {
                    if (!rdr.Read()) break;
                    if (rdr.TokenType != System.Text.Json.JsonTokenType.StartArray) break;
                    while (rdr.Read() && rdr.TokenType != System.Text.Json.JsonTokenType.EndArray)
                    {
                        if (rdr.TokenType == System.Text.Json.JsonTokenType.StartObject)
                        {
                            try
                            {
                                var el = System.Text.Json.JsonDocument.ParseValue(ref rdr).RootElement;
                                var d = el.Deserialize<Drama>(JsonOpts);
                                if (d != null) outList.Add(d);
                            }
                            catch { }
                        }
                    }
                    break;
                }
            }
        }
        catch { }
        return outList;
    }

    private async Task<string> SendRawAsync(string method, string path, object? body, bool skipViewer)
    {
        LogX(method + " " + path);
        using var req = Build(method, path, body, skipViewer);
        using var resp = await _http.SendAsync(req);
        var text = await resp.Content.ReadAsStringAsync();
        if (!resp.IsSuccessStatusCode)
        {
            string msg = "HTTP " + (int)resp.StatusCode;
            try
            {
                using var doc = System.Text.Json.JsonDocument.Parse(string.IsNullOrWhiteSpace(text) ? "{}" : text);
                if (doc.RootElement.TryGetProperty("error", out var e)) msg = e.GetString() ?? msg;
            }
            catch { }
            throw new Exception(msg);
        }
        return text;
    }

    public async Task<(List<Drama> Data, int Total)> Search(string q)
    {
        var res = await SendAsync<SearchResponse>("GET", $"/api/ui/search?q={Uri.EscapeDataString(q)}");
        return (res.Data ?? new(), res.Total);
    }

    public async Task<PlaybackOpenResult> PlaybackOpen(string dramaId, bool resume = true)
    {
        return await SendAsync<PlaybackOpenResult>("POST", "/api/ui/playback/open",
            new { dramaId, resume, fromHistory = false });
    }

    public async Task<HlsOpenResult> HlsOpen(string session, int episode, double? start = null)
    {
        object body = start is > 0
            ? new { session, episode, start }
            : new { session, episode };
        return await SendAsync<HlsOpenResult>("POST", "/api/ui/playback/hls/open", body);
    }

    public async Task PlaybackControl(string session, string action)
    {
        await SendAsync<JsonElement>("POST", "/api/ui/playback/control", new { session, action });
    }

    public async Task PlaybackProgress(string session, long run, long sequence, int episode,
        double position, double duration, bool completed)
    {
        await SendAsync<JsonElement>("POST", "/api/ui/playback/progress", new
        {
            session,
            progress = new { run, sequence, episode, position, duration, completed }
        });
    }

    public string ResolveUrl(string? pathOrUrl)
    {
        if (string.IsNullOrWhiteSpace(pathOrUrl)) return "";
        if (pathOrUrl.StartsWith("http://", StringComparison.OrdinalIgnoreCase) ||
            pathOrUrl.StartsWith("https://", StringComparison.OrdinalIgnoreCase))
            return pathOrUrl;
        var b = _baseUrl.TrimEnd('/');
        return pathOrUrl.StartsWith('/') ? b + pathOrUrl : $"{b}/{pathOrUrl}";
    }

    public string CoverUrl(string? cover)
    {
        if (string.IsNullOrWhiteSpace(cover)) return "";
        // 绝对 URL 直连（黄果/黄豆 CDN）；相对路径拼 base
        return ResolveUrl(cover);
    }

    private sealed class LoginResponse
    {
        public bool Ok { get; set; }
        public AccountInfo? Account { get; set; }
    }

    private sealed class SearchResponse
    {
        public List<Drama>? Data { get; set; }
        public int Total { get; set; }
    }

    /// <summary>带 Cookie 下载封面/图片字节</summary>
    public async Task<byte[]?> DownloadBytesAsync(string url)
    {
        if (string.IsNullOrEmpty(url)) return null;
        using var req = new HttpRequestMessage(HttpMethod.Get, url);
        req.Headers.TryAddWithoutValidation("Sec-Fetch-Site", "none");
        req.Headers.TryAddWithoutValidation("User-Agent", "Mozilla/5.0 (JukuWinUI)");
        if (!string.IsNullOrEmpty(_viewerId))
            req.Headers.TryAddWithoutValidation("X-Juku-Viewer", _viewerId);
        using var resp = await _http.SendAsync(req);
        if (!resp.IsSuccessStatusCode) return null;
        return await resp.Content.ReadAsByteArrayAsync();
    }
}
