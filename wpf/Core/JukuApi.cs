using System.Net.Http;
using System.Net;
using System.Text;
using System.Text.Json;

namespace JukuWpf.Core;

public sealed class JukuApi
{
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
            Timeout = TimeSpan.FromSeconds(30),
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
        return await SendAsync<DramasResponse>("GET", path);
    }

    public async Task<(List<Drama> Data, int Total)> Search(string q)
    {
        var res = await SendAsync<SearchResponse>("GET", $"/api/ui/search?q={Uri.EscapeDataString(q)}");
        return (res.Data ?? new(), res.Total);
    }

        public async Task<PlaybackOpenResult> PlaybackOpen(string dramaId, bool resume = true)
    {
        try
        {
            return await SendAsync<PlaybackOpenResult>("POST", "/api/ui/playback/open", new { dramaId, resume, fromHistory = false });
        }
        catch (Exception ex) when (ex.Message.Contains("身份") || ex.Message.Contains("Cookie") || ex.Message.Contains("刷新"))
        {
            await EnsureViewer();
            return await SendAsync<PlaybackOpenResult>("POST", "/api/ui/playback/open", new { dramaId, resume, fromHistory = false });
        }
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

    /// <summary>带 Cookie 下载图片字节（封面必须走登录态）</summary>
    public async Task<byte[]?> DownloadBytesAsync(string url)
    {
        if (string.IsNullOrEmpty(url)) return null;
        using var req = new HttpRequestMessage(HttpMethod.Get, url);
        req.Headers.TryAddWithoutValidation("Sec-Fetch-Site", "none");
        if (!string.IsNullOrEmpty(_viewerId))
            req.Headers.TryAddWithoutValidation("X-Juku-Viewer", _viewerId);
        using var resp = await _http.SendAsync(req);
        if (!resp.IsSuccessStatusCode) return null;
        return await resp.Content.ReadAsByteArrayAsync();
    }


    public string BuildStreamUrl(string session, int episode)
    {
        var b = _baseUrl.TrimEnd('/');
        return b + "/api/ui/playback/stream?session=" + Uri.EscapeDataString(session)
             + "&episode=" + episode;
    }

    /// <summary>身份失效时重新拉 viewer 并更新 X-Juku-Viewer</summary>
    public async Task RefreshIdentityAsync()
    {
        await EnsureViewer();
    }


    public string BuildHlsUrl(string session, int episode)
    {
        var b = _baseUrl.TrimEnd('/');
        return b + "/api/ui/playback/hls/index.m3u8?session=" + Uri.EscapeDataString(session)
             + "&run=1&episode=" + episode;
    }

    /// <summary>给本地代理用：发请求并保留 Cookie/身份头</summary>
    public Task<HttpResponseMessage> SendRawForProxyAsync(HttpRequestMessage req)
    {
        return _http.SendAsync(req, HttpCompletionOption.ResponseHeadersRead);
    }

    public string BuildHlsUrlWithRun(string session, long run, int episode)
    {
        var b = _baseUrl.TrimEnd('/');
        return b + "/api/ui/playback/hls/index.m3u8?session=" + Uri.EscapeDataString(session)
             + "&run=" + run + "&episode=" + episode;
    }

    public string BuildStreamUrlWithQuery(string session, int episode, long run = 0)
    {
        var b = _baseUrl.TrimEnd('/');
        var u = b + "/api/ui/playback/stream?session=" + Uri.EscapeDataString(session) + "&episode=" + episode;
        if (run != 0) u += "&run=" + run;
        return u;
    }
}
