using JukuWinUI.Core;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Input;
using Microsoft.UI.Xaml.Controls.Primitives;
using Microsoft.UI.Xaml.Navigation;
using Windows.Media.Core;
using Windows.Media.Playback;
using Windows.System;

namespace JukuWinUI.Views;

public sealed partial class PlayerPage : Page
{
    private Drama _drama = new();
    private int _episode = 1;
    private string _session = "";
    private long _run;
    private long _seq;
    private DispatcherTimer? _heartbeat;
    private DispatcherTimer? _progress;
    private DispatcherTimer? _uiTimer;
    private MediaPlayer? _player;
    private bool _suppressEp;

    public PlayerPage()
    {
        InitializeComponent();
    }

    protected override async void OnNavigatedTo(NavigationEventArgs e)
    {
        base.OnNavigatedTo(e);
        if (e.Parameter is (Drama d, int ep))
        {
            _drama = d;
            _episode = ep;
        }
        TitleText.Text = _drama.Title;
        EpText.Text = $"第{_episode}集";
        await OpenAsync();
    }

    protected override void OnNavigatedFrom(NavigationEventArgs e)
    {
        base.OnNavigatedFrom(e);
        StopTimers();
        try { _player?.Pause(); } catch { }
        if (!string.IsNullOrEmpty(_session))
            _ = MainWindow.Api.PlaybackControl(_session, "close");
    }

    private async Task OpenAsync()
    {
        LoadingText.Visibility = Visibility.Visible;
        try
        {
            if (string.IsNullOrEmpty(_session))
            {
                var open = await MainWindow.Api.PlaybackOpen(_drama.Id, true);
                _session = open.Session;
                _suppressEp = true;
                EpisodeList.Items.Clear();
                var select = 0;
                for (var i = 0; i < open.Episodes.Count; i++)
                {
                    var ep = open.Episodes[i];
                    EpisodeList.Items.Add(new TextBlock { Text = ep.Title, Tag = ep.EpisodeNumber });
                    if (ep.EpisodeNumber == _episode) select = i;
                }
                if (EpisodeList.Items.Count > 0) EpisodeList.SelectedIndex = select;
                _suppressEp = false;
            }

            var hls = await MainWindow.Api.HlsOpen(_session, _episode);
            _run = hls.Run;
            var url = MainWindow.Api.ResolveUrl(hls.Url);
            EpText.Text = $"第{_episode}集";

            _player = new MediaPlayer
            {
                Source = MediaSource.CreateFromUri(new Uri(url)),
                AutoPlay = true,
            };
            _player.PlaybackSession.PlaybackStateChanged += (_, _) =>
            {
                DispatcherQueue.TryEnqueue(() =>
                {
                    var s = _player?.PlaybackSession.PlaybackState;
                    PlayPauseBtn.Content = s == MediaPlaybackState.Playing ? "暂停" : "播放";
                    if (s == MediaPlaybackState.Playing) StartTimers();
                });
            };
            Player.SetMediaPlayer(_player);
        }
        catch (Exception ex)
        {
            LoadingText.Text = ex.Message;
        }
        finally
        {
            if (_player != null) LoadingText.Visibility = Visibility.Collapsed;
        }
    }

    private void StartTimers()
    {
        if (_uiTimer == null)
        {
            _uiTimer = new DispatcherTimer { Interval = TimeSpan.FromMilliseconds(300) };
            _uiTimer.Tick += (_, _) =>
            {
                if (_player == null) return;
                var pos = _player.PlaybackSession.Position.TotalMilliseconds;
                var dur = _player.PlaybackSession.NaturalDuration.TotalMilliseconds;
                if (dur > 0)
                {
                    var pct = Math.Clamp(pos / dur, 0, 1);
                    SeekFill.Width = SeekTrack.ActualWidth * pct;
                    TimeText.Text = $"{Fmt(pos)} / {Fmt(dur)}";
                }
                else
                {
                    TimeText.Text = $"{Fmt(pos)} / --:--";
                }
            };
        }
        _uiTimer.Start();

        if (_heartbeat == null)
        {
            _heartbeat = new DispatcherTimer { Interval = TimeSpan.FromSeconds(60) };
            _heartbeat.Tick += async (_, _) =>
            {
                if (string.IsNullOrEmpty(_session)) return;
                try { await MainWindow.Api.PlaybackControl(_session, "heartbeat"); } catch { }
            };
        }
        _heartbeat.Start();
    }

    private void StopTimers()
    {
        _uiTimer?.Stop();
        _heartbeat?.Stop();
        _progress?.Stop();
        _uiTimer = null;
        _heartbeat = null;
        _progress = null;
    }

    private static string Fmt(double ms)
    {
        var s = Math.Max(0, ms) / 1000.0;
        var h = (int)(s / 3600);
        var m = (int)((s % 3600) / 60);
        var sec = (int)(s % 60);
        return h > 0 ? $"{h}:{m:00}:{sec:00}" : $"{m:00}:{sec:00}";
    }

    private void PlayPause_Click(object sender, RoutedEventArgs e)
    {
        if (_player == null) return;
        if (_player.PlaybackSession.PlaybackState == MediaPlaybackState.Playing) _player.Pause();
        else _player.Play();
    }

    private void SeekBy_Click(object sender, RoutedEventArgs e)
    {
        if (_player == null || sender is not Button b) return;
        if (!double.TryParse(b.Tag?.ToString(), out var sec)) return;
        var cur = _player.PlaybackSession.Position;
        var next = cur + TimeSpan.FromSeconds(sec);
        if (next < TimeSpan.Zero) next = TimeSpan.Zero;
        _player.PlaybackSession.Position = next;
    }

    private void SeekTrack_PointerPressed(object sender, PointerRoutedEventArgs e)
    {
        if (_player == null) return;
        var pt = e.GetCurrentPoint(SeekTrack).Position;
        var pct = Math.Clamp(pt.X / Math.Max(1, SeekTrack.ActualWidth), 0, 1);
        var dur = _player.PlaybackSession.NaturalDuration;
        if (dur > TimeSpan.Zero)
            _player.PlaybackSession.Position = TimeSpan.FromMilliseconds(dur.TotalMilliseconds * pct);
    }

    private void VolumeSlider_ValueChanged(object sender, RangeBaseValueChangedEventArgs e)
    {
        if (_player != null) _player.Volume = e.NewValue / 100.0;
    }

    private void Speed_Click(object sender, RoutedEventArgs e)
    {
        if (_player == null || sender is not Button b) return;
        if (double.TryParse(b.Tag?.ToString(), out var rate))
            _player.PlaybackSession.PlaybackRate = rate;
    }

    private void Fullscreen_Click(object sender, RoutedEventArgs e)
    {
        // WinUI 全屏：交给 AppWindow
        if (App.MainWindow is MainWindow mw) mw.ToggleFullscreen();
    }

    private void MinBtn_Click(object sender, RoutedEventArgs e)
    {
        if (App.MainWindow is MainWindow mw) mw.Minimize();
    }

    private void CloseBtn_Click(object sender, RoutedEventArgs e)
    {
        if (Frame.CanGoBack) Frame.GoBack();
    }

    private void ToggleEpisodes_Click(object sender, RoutedEventArgs e)
        => Drawer.Visibility = Drawer.Visibility == Visibility.Visible ? Visibility.Collapsed : Visibility.Visible;

    private async void EpisodeList_SelectionChanged(object sender, SelectionChangedEventArgs e)
    {
        if (_suppressEp) return;
        if (EpisodeList.SelectedItem is not TextBlock item) return;
        if (item.Tag is not int ep || ep == _episode) return;
        _episode = ep;
        await OpenAsync();
    }
}
