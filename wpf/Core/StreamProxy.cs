using System.Net;
using System.Net.Http;

namespace JukuWpf.Core;

/// <summary>
/// 路径透传本地代理：补 Cookie / X-Juku-Viewer。
/// m3u8 相对分片会走同一代理，内嵌 VLC/LibVLC 可直接播。
/// </summary>
public sealed class StreamProxy : IDisposable
{
    private readonly JukuApi _api;
    private HttpListener? _listener;
    private int _port;

    public StreamProxy(JukuApi api) => _api = api;

    public int Port => _port;

    public string BaseUrl => $"http://127.0.0.1:{_port}";

    public string Start()
    {
        for (var p = 18765; p < 18860; p++)
        {
            try
            {
                var l = new HttpListener();
                l.Prefixes.Add($"http://127.0.0.1:{p}/");
                l.Start();
                _listener = l;
                _port = p;
                _ = Task.Run(LoopAsync);
                return BaseUrl;
            }
            catch
            {
                // try next port
            }
        }
        throw new Exception("本地代理端口都被占用");
    }

    private async Task LoopAsync()
    {
        if (_listener == null) return;
        while (_listener.IsListening)
        {
            HttpListenerContext ctx;
            try { ctx = await _listener.GetContextAsync(); }
            catch { break; }
            _ = Task.Run(() => HandleAsync(ctx));
        }
    }

    private async Task HandleAsync(HttpListenerContext ctx)
    {
        try
        {
            var pathAndQuery = ctx.Request.Url?.PathAndQuery ?? "/";
            var target = _api.ResolveUrl(pathAndQuery);

            using var req = new HttpRequestMessage(HttpMethod.Get, target);
            req.Headers.TryAddWithoutValidation("Sec-Fetch-Site", "none");
            req.Headers.TryAddWithoutValidation("User-Agent", "Mozilla/5.0 (JukuDesktop)");
            if (!string.IsNullOrEmpty(_api.ViewerId))
                req.Headers.TryAddWithoutValidation("X-Juku-Viewer", _api.ViewerId);

            using var resp = await _api.SendRawForProxyAsync(req);
            ctx.Response.StatusCode = (int)resp.StatusCode;
            var ct = resp.Content.Headers.ContentType?.ToString();
            if (!string.IsNullOrEmpty(ct)) ctx.Response.ContentType = ct;
            if (pathAndQuery.Contains(".m3u8", StringComparison.OrdinalIgnoreCase))
                ctx.Response.ContentType = "application/vnd.apple.mpegurl";
            ctx.Response.Headers["Access-Control-Allow-Origin"] = "*";

            await using var body = await resp.Content.ReadAsStreamAsync();
            await body.CopyToAsync(ctx.Response.OutputStream);
            try { ctx.Response.OutputStream.Close(); } catch { }
        }
        catch
        {
            try { ctx.Response.StatusCode = 502; ctx.Response.Close(); } catch { }
        }
    }

    /// <summary>后端相对/绝对地址 → 本地代理地址</summary>
    public string ToProxyUrl(string backendPathOrUrl)
    {
        if (string.IsNullOrEmpty(backendPathOrUrl)) return "";
        string path;
        if (backendPathOrUrl.StartsWith("http", StringComparison.OrdinalIgnoreCase))
        {
            var u = new Uri(backendPathOrUrl);
            path = u.PathAndQuery;
        }
        else
        {
            path = backendPathOrUrl.StartsWith("/") ? backendPathOrUrl : "/" + backendPathOrUrl;
        }
        return BaseUrl + path;
    }

    public void Dispose()
    {
        try { _listener?.Stop(); _listener?.Close(); } catch { }
        _listener = null;
    }
}
