using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Media;
using System.Windows.Threading;
using JukuWpf.Core;

namespace JukuWpf;

public partial class PlayerWindow : Window
{
    private readonly JukuApi _api;
    private readonly Drama _drama;
    private StreamProxy? _proxy;
    private string _session = "";
    private int _episode;
    private bool _suppress;
    private DispatcherTimer? _uiTimer;
    private DispatcherTimer? _heartbeat;

    public PlayerWindow(JukuApi api, Drama drama, int episode = 1)
    {
        InitializeComponent();
        _api = api;
        _drama = drama;
        _episode = episode;
        TitleText.Text = drama.Title;
        Title = $"短剧库 · {drama.Title}";
        SetSpeedUi(1.0);
        _ = OpenAsync();
    }

    private void SetSpeedUi(double rate)
    {
        foreach (var (btn, v) in new (Button?, double)[] {
            (Sp05, 0.5), (Sp1, 1), (Sp15, 1.5), (Sp2, 2) })
        {
            if (btn == null) continue;
            var on = Math.Abs(v - rate) < 0.01;
            btn.Background = new SolidColorBrush(on ? Color.FromRgb(0x7C, 0x6A, 0xF7) : Colors.Transparent);
            btn.Foreground = Brushes.White;
        }
    }

    private async Task OpenAsync()
    {
        LoadingText.Visibility = Visibility.Visible;
        try
        {
            _proxy ??= new StreamProxy(_api);
            if (_proxy.Port == 0) _proxy.Start();
            if (string.IsNullOrEmpty(_session))
            {
                var open = await _api.PlaybackOpen(_drama.Id, resume: true);
                _session = open.Session;
                _suppress = true;
                EpisodeList.Items.Clear();
                var select = 0;
                for (var i = 0; i < open.Episodes.Count; i++)
                {
                    var ep = open.Episodes[i];
                    EpisodeList.Items.Add(new ListBoxItem { Content = ep.Title, Tag = ep.EpisodeNumber });
                    if (ep.EpisodeNumber == _episode) select = i;
                }
                if (EpisodeList.Items.Count > 0) EpisodeList.SelectedIndex = select;
                _suppress = false;
            }
            var url = _proxy.ToProxyUrl($"/api/ui/playback/stream?session={Uri.EscapeDataString(_session)}&episode={_episode}");
            Player.Stop();
            Player.Source = new Uri(url, UriKind.Absolute);
            Player.Play();
        }
        catch (Exception ex)
        {
            LoadingText.Text = ex.Message;
        }
    }

    private void Player_MediaOpened(object sender, RoutedEventArgs e)
    {
        LoadingText.Visibility = Visibility.Collapsed;
        PlayPauseBtn.Content = "暂停";
        StartTimers();
    }

    private void Player_MediaEnded(object sender, RoutedEventArgs e) => PlayPauseBtn.Content = "播放";

    private void Player_MediaFailed(object sender, ExceptionRoutedEventArgs e)
        => LoadingText.Text = "播放失败";

    private void StartTimers()
    {
        if (_uiTimer != null) return;
        _uiTimer = new DispatcherTimer { Interval = TimeSpan.FromMilliseconds(200) };
        _uiTimer.Tick += (_, _) => UpdateTimeUi();
        _uiTimer.Start();

        _heartbeat = new DispatcherTimer { Interval = TimeSpan.FromSeconds(60) };
        _heartbeat.Tick += async (_, _) =>
        {
            if (string.IsNullOrEmpty(_session)) return;
            try { await _api.PlaybackControl(_session, "heartbeat"); } catch { }
        };
        _heartbeat.Start();
    }

    private void UpdateTimeUi()
    {
        var pos = Player.Position;
        // NaturalDuration 对 fMP4 可能不给，用 Clock/估算
        TimeSpan dur = Player.NaturalDuration.HasTimeSpan
            ? Player.NaturalDuration.TimeSpan
            : TimeSpan.Zero;
        if (dur <= TimeSpan.Zero && Player.Clock?.NaturalDuration.HasTimeSpan == true)
            dur = Player.Clock.NaturalDuration.TimeSpan;

        var pct = dur > TimeSpan.Zero ? pos.TotalMilliseconds / dur.TotalMilliseconds : 0;
        pct = Math.Clamp(pct, 0, 1);
        var trackW = SeekTrack.ActualWidth;
        if (trackW > 0) SeekFill.Width = trackW * pct;

        TimeText.Text = dur > TimeSpan.Zero
            ? $"{Fmt(pos.TotalMilliseconds)} / {Fmt(dur.TotalMilliseconds)}"
            : $"{Fmt(pos.TotalMilliseconds)} / --:--";
    }

    private static string Fmt(double ms)
    {
        var s = Math.Max(0, ms) / 1000.0;
        var h = (int)(s / 3600);
        var m = (int)((s % 3600) / 60);
        var sec = (int)(s % 60);
        return h > 0 ? $"{h}:{m:00}:{sec:00}" : $"{m:00}:{sec:00}";
    }

    private void PlayPause_Click(object sender, RoutedEventArgs e) => TogglePlay();

    private void TogglePlay()
    {
        if (PlayPauseBtn.Content.ToString() == "暂停")
        {
            Player.Pause();
            PlayPauseBtn.Content = "播放";
        }
        else
        {
            Player.Play();
            PlayPauseBtn.Content = "暂停";
        }
    }

    private void SeekBy_Click(object sender, RoutedEventArgs e)
    {
        if (sender is not Button b) return;
        if (!double.TryParse(b.Tag?.ToString(), out var sec)) return;
        var next = Player.Position + TimeSpan.FromSeconds(sec);
        Player.Position = next < TimeSpan.Zero ? TimeSpan.Zero : next;
        UpdateTimeUi();
    }

    private void SeekGrid_Click(object sender, MouseButtonEventArgs e)
    {
        var fe = sender as FrameworkElement;
        if (fe == null) return;
        var pos = e.GetPosition(fe).X / Math.Max(1, fe.ActualWidth);
        pos = Math.Clamp(pos, 0, 1);
        var dur = Player.NaturalDuration.HasTimeSpan
            ? Player.NaturalDuration.TimeSpan
            : Player.Clock?.NaturalDuration.HasTimeSpan == true
                ? Player.Clock.NaturalDuration.TimeSpan
                : TimeSpan.Zero;
        if (dur > TimeSpan.Zero)
            Player.Position = TimeSpan.FromMilliseconds(dur.TotalMilliseconds * pos);
    }

    private void VolumeSlider_ValueChanged(object sender, RoutedPropertyChangedEventArgs<double> e)
        => Player.Volume = e.NewValue / 100.0;

    private void Speed_Click(object sender, RoutedEventArgs e)
    {
        if (sender is not Button b) return;
        if (double.TryParse(b.Tag?.ToString(), out var rate))
        {
            Player.SpeedRatio = rate;
            SetSpeedUi(rate);
        }
    }

    private void Window_PreviewKeyDown(object sender, KeyEventArgs e)
    {
        switch (e.Key)
        {
            case Key.Space:
                TogglePlay();
                e.Handled = true;
                break;
            case Key.F:
                Fullscreen_Click(sender, e);
                e.Handled = true;
                break;
            case Key.Left:
                Player.Position = Player.Position > TimeSpan.FromSeconds(10)
                    ? Player.Position - TimeSpan.FromSeconds(10) : TimeSpan.Zero;
                e.Handled = true;
                break;
            case Key.Right:
                Player.Position += TimeSpan.FromSeconds(10);
                e.Handled = true;
                break;
            case Key.Escape:
                if (WindowState == WindowState.Maximized)
                {
                    WindowState = WindowState.Normal;
                    e.Handled = true;
                }
                break;
        }
    }

    private void TopBar_MouseLeftButtonDown(object sender, MouseButtonEventArgs e)
    {
        if (e.ClickCount == 2) Fullscreen_Click(sender, e);
        else DragMove();
    }

    private void Fullscreen_Click(object sender, RoutedEventArgs e)
        => WindowState = WindowState == WindowState.Maximized ? WindowState.Normal : WindowState.Maximized;

    private void Min_Click(object sender, RoutedEventArgs e) => WindowState = WindowState.Minimized;
    private void Max_Click(object sender, RoutedEventArgs e) => Fullscreen_Click(sender, e);

    private void ToggleEpisodes_Click(object sender, RoutedEventArgs e)
        => Drawer.Visibility = Drawer.Visibility == Visibility.Visible ? Visibility.Collapsed : Visibility.Visible;

    private async void EpisodeList_SelectionChanged(object sender, SelectionChangedEventArgs e)
    {
        if (_suppress) return;
        if (EpisodeList.SelectedItem is not ListBoxItem item) return;
        if (item.Tag is not int ep || ep == _episode) return;
        _episode = ep;
        await OpenAsync();
    }

    private void Close_Click(object sender, RoutedEventArgs e) => Close();

    protected override void OnClosed(EventArgs e)
    {
        _uiTimer?.Stop();
        _heartbeat?.Stop();
        try { Player.Stop(); } catch { }
        var s = _session;
        _session = "";
        var proxy = _proxy;
        _proxy = null;
        _ = Task.Run(async () =>
        {
            try { if (!string.IsNullOrEmpty(s)) await _api.PlaybackControl(s, "close"); } catch { }
            proxy?.Dispose();
        });
        base.OnClosed(e);
    }
}
