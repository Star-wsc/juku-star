using System.Text.Json;
using System.Text.Json.Serialization;

namespace JukuWpf.Core;

/// <summary>宽松解析 JSON 数值（兼容 number / string / null）</summary>
public sealed class FlexIntConverter : JsonConverter<int>
{
    public override int Read(ref Utf8JsonReader reader, Type typeToConvert, JsonSerializerOptions options)
    {
        if (reader.TokenType == JsonTokenType.Number && reader.TryGetInt32(out var n)) return n;
        if (reader.TokenType == JsonTokenType.String)
        {
            var s = reader.GetString();
            return int.TryParse(s, out var v) ? v : (int.TryParse(s?.Split('.')[0], out var v2) ? v2 : 0);
        }
        if (reader.TokenType == JsonTokenType.Null) return 0;
        if (reader.TokenType == JsonTokenType.Number)
        {
            return (int)reader.GetDouble();
        }
        reader.Skip();
        return 0;
    }

    public override void Write(Utf8JsonWriter writer, int value, JsonSerializerOptions options)
        => writer.WriteNumberValue(value);
}

public sealed class FlexLongConverter : JsonConverter<long>
{
    public override long Read(ref Utf8JsonReader reader, Type typeToConvert, JsonSerializerOptions options)
    {
        if (reader.TokenType == JsonTokenType.Number && reader.TryGetInt64(out var n)) return n;
        if (reader.TokenType == JsonTokenType.String)
        {
            var s = reader.GetString();
            return long.TryParse(s, out var v) ? v : 0;
        }
        if (reader.TokenType == JsonTokenType.Number) return (long)reader.GetDouble();
        if (reader.TokenType == JsonTokenType.Null) return 0;
        reader.Skip();
        return 0;
    }

    public override void Write(Utf8JsonWriter writer, long value, JsonSerializerOptions options)
        => writer.WriteNumberValue(value);
}

public sealed class FlexDoubleConverter : JsonConverter<double>
{
    public override double Read(ref Utf8JsonReader reader, Type typeToConvert, JsonSerializerOptions options)
    {
        if (reader.TokenType == JsonTokenType.Number) return reader.GetDouble();
        if (reader.TokenType == JsonTokenType.String)
        {
            var s = reader.GetString();
            return double.TryParse(s, out var v) ? v : 0;
        }
        if (reader.TokenType == JsonTokenType.Null) return 0;
        reader.Skip();
        return 0;
    }

    public override void Write(Utf8JsonWriter writer, double value, JsonSerializerOptions options)
        => writer.WriteNumberValue(value);
}

public sealed class FlexBoolConverter : JsonConverter<bool>
{
    public override bool Read(ref Utf8JsonReader reader, Type typeToConvert, JsonSerializerOptions options)
    {
        if (reader.TokenType == JsonTokenType.True) return true;
        if (reader.TokenType == JsonTokenType.False) return false;
        if (reader.TokenType == JsonTokenType.String)
        {
            var s = reader.GetString();
            return s is "1" or "true" or "True" or "yes";
        }
        if (reader.TokenType == JsonTokenType.Number) return reader.GetInt32() != 0;
        if (reader.TokenType == JsonTokenType.Null) return false;
        reader.Skip();
        return false;
    }

    public override void Write(Utf8JsonWriter writer, bool value, JsonSerializerOptions options)
        => writer.WriteBooleanValue(value);
}

public sealed class Viewer
{
    [JsonPropertyName("ready")] public bool Ready { get; set; }
    [JsonPropertyName("id")] public string Id { get; set; } = "";
    [JsonPropertyName("account")] public AccountInfo? Account { get; set; }
    [JsonPropertyName("sources")] public List<string> Sources { get; set; } = new();
    [JsonPropertyName("requireLogin")] public bool RequireLogin { get; set; }
    [JsonPropertyName("allowRegistration")] public bool AllowRegistration { get; set; }
}

public sealed class AccountInfo
{
    [JsonPropertyName("username")] public string Username { get; set; } = "";
    [JsonPropertyName("admin")] public bool Admin { get; set; }
}

public sealed class Drama
{
    [JsonPropertyName("id")] public string Id { get; set; } = "";
    [JsonPropertyName("source")] public string Source { get; set; } = "";
    [JsonPropertyName("title")] public string Title { get; set; } = "";
    [JsonPropertyName("cover")] public string Cover { get; set; } = "";
    [JsonPropertyName("desc")] public string Desc { get; set; } = "";
    [JsonPropertyName("totalEpisode")]
    [JsonConverter(typeof(FlexIntConverter))]
    public int TotalEpisode { get; set; }
    [JsonPropertyName("category")] public string Category { get; set; } = "";
    [JsonPropertyName("releaseStatus")] public string ReleaseStatus { get; set; } = "";
    [JsonPropertyName("vip")]
    [JsonConverter(typeof(FlexBoolConverter))]
    public bool Vip { get; set; }
    [JsonPropertyName("score")] public string Score { get; set; } = "";
    [JsonPropertyName("views")] public string Views { get; set; } = "";
    [JsonPropertyName("tags")] public List<string> Tags { get; set; } = new();
    [JsonPropertyName("onlineDate")] public string OnlineDate { get; set; } = "";
}

public sealed class DramasResponse
{
    [JsonPropertyName("data")] public List<Drama> Data { get; set; } = new();
    [JsonPropertyName("total")]
    [JsonConverter(typeof(FlexIntConverter))]
    public int Total { get; set; }
    [JsonPropertyName("hasMore")] public bool HasMore { get; set; }
}

public sealed class Episode
{
    [JsonPropertyName("vip")]
    [JsonConverter(typeof(FlexBoolConverter))]
    public bool Vip { get; set; }
    [JsonPropertyName("index")]
    [JsonConverter(typeof(FlexIntConverter))]
    public int Index { get; set; }
    [JsonPropertyName("episode")] public JsonElement EpisodeRaw { get; set; }
    [JsonPropertyName("title")] public string Title { get; set; } = "";
    [JsonPropertyName("number")]
    [JsonConverter(typeof(FlexIntConverter))]
    public int Number { get; set; }
    [JsonPropertyName("total")]
    [JsonConverter(typeof(FlexIntConverter))]
    public int Total { get; set; }

    public int EpisodeNumber
    {
        get
        {
            if (EpisodeRaw.ValueKind == JsonValueKind.Number && EpisodeRaw.TryGetInt32(out var n))
                return n;
            if (EpisodeRaw.ValueKind == JsonValueKind.String &&
                int.TryParse(EpisodeRaw.GetString(), out var s))
                return s;
            return Index;
        }
    }
}

public sealed class PlaybackOpenResult
{
    [JsonPropertyName("session")] public string Session { get; set; } = "";
    [JsonPropertyName("dramaId")] public string DramaId { get; set; } = "";
    [JsonPropertyName("title")] public string Title { get; set; } = "";
    [JsonPropertyName("episodes")] public List<Episode> Episodes { get; set; } = new();
    [JsonPropertyName("initialIndex")]
    [JsonConverter(typeof(FlexIntConverter))]
    public int InitialIndex { get; set; }
    [JsonPropertyName("initialPosition")]
    [JsonConverter(typeof(FlexDoubleConverter))]
    public double InitialPosition { get; set; }
    [JsonPropertyName("resumeMessage")] public string? ResumeMessage { get; set; }
}

public sealed class HlsOpenResult
{
    [JsonPropertyName("url")] public string Url { get; set; } = "";
    [JsonPropertyName("run")]
    [JsonConverter(typeof(FlexLongConverter))]
    public long Run { get; set; }
    [JsonPropertyName("duration")]
    [JsonConverter(typeof(FlexDoubleConverter))]
    public double Duration { get; set; }
    [JsonPropertyName("quality")]
    [JsonConverter(typeof(FlexIntConverter))]
    public int Quality { get; set; }
}
