using System.IO;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Media;
using System.Windows.Media.Imaging;
using System.Windows.Threading;
using JukuWpf.Core;

namespace JukuWpf;

public partial class MainWindow : Window
{
    private static readonly JukuApi Api = new();
    private static AppConfig Config = new();
    private List<Drama> _all = new();
    private List<Drama> _filtered = new();
    private bool _hideVip;
    private string _source = "hongguo";
    private string _sort = "random";
    private int _pageSize = 40;
    private int _shown;
    private static readonly Dictionary<string, BitmapImage> CoverCache = new();
    private static readonly SemaphoreSlim CoverGate = new(4, 4);

    public MainWindow()
    {
        InitializeComponent();
        Config = AppConfig.Load();
        if (Config.HasCredentials)
        {
            ServerBox.Text = Config.ServerUrl;
            UserBox.Text = Config.Username;
            PassBox.Password = Config.Password;
            _ = AutoLoginAsync();
        }
    }

    private async Task AutoLoginAsync()
    {
        try
        {
            Api.SetBaseUrl(AppConfig.NormalizeServerUrl(Config.ServerUrl));
            await Api.Login(Config.Username, Config.Password);
            ShowMain();
            await LoadLibraryAsync(false);
        }
        catch { }
    }

    private void ShowMain()
    {
        LoginPanel.Visibility = Visibility.Collapsed;
        MainPanel.Visibility = Visibility.Visible;
        TitleText.Text = "短剧库";
        ServerLabel.Text = Config.ServerUrl.Replace("http://", "").Replace("https://", "");
        AccountLabel.Text = Config.Username;
        SetUser.Text = Config.Username;
        SetServer.Text = "服务器 " + Config.ServerUrl;
        ChkHuangguo.IsChecked = Config.ShowHuangguo;
        ChkHuangdou.IsChecked = Config.ShowHuangdou;
        LibraryView.Visibility = Visibility.Visible;
        SettingsView.Visibility = Visibility.Collapsed;
    }

    private async Task LoadLibraryAsync(bool refresh)
    {
        try
        {
            SubText.Text = "加载中…";
            var res = await Api.FetchDramas(refresh);
            _all = res.Data ?? new();
            Recalculate();
        }
        catch (Exception ex)
        {
            SubText.Text = "加载失败：" + ex.Message;
        }
    }

    private void Recalculate()
    {
        IEnumerable<Drama> list = _all.Where(d => !Config.IsSourceHidden(d.Source));
        if (_source != "all")
            list = list.Where(d => AppConfig.SourceGroup(d.Source) == _source || d.Source == _source);
        if (_hideVip) list = list.Where(d => !d.Vip);
        _filtered = list.ToList();
        if (_sort == "random") _filtered = _filtered.OrderBy(_ => Random.Shared.Next()).ToList();

        DramaList.Items.Clear();
        _shown = 0;
        AppendPage();
    }

    private void AppendPage()
    {
        var next = _filtered.Skip(_shown).Take(_pageSize).ToList();
        foreach (var d in next)
            DramaList.Items.Add(BuildCard(d));
        _shown += next.Count;
        SubText.Text = _shown < _filtered.Count
            ? $"发现更多好看的故事 · 共 {_filtered.Count:N0} 部（已显示 {_shown}，滚到底加载更多）"
            : $"发现更多好看的故事 · 共 {_filtered.Count:N0} 部";
    }

    private void ScrollViewer_ScrollChanged(object sender, ScrollChangedEventArgs e)
    {
        if (sender is not ScrollViewer sv) return;
        if (sv.VerticalOffset + sv.ViewportHeight >= sv.ExtentHeight - 120 && _shown < _filtered.Count)
            AppendPage();
    }

    private UIElement BuildCard(Drama d)
    {
        // 去掉 DropShadow —— WPF 阴影极吃性能
        var card = new Border
        {
            Width = 168,
            Margin = new Thickness(0, 0, 16, 16),
            Background = Brushes.White,
            CornerRadius = new CornerRadius(14),
            BorderBrush = new SolidColorBrush(Color.FromRgb(0xE5, 0xE5, 0xEA)),
            BorderThickness = new Thickness(1),
            Cursor = Cursors.Hand,
        };

        var root = new StackPanel();
        var poster = new Border
        {
            Width = 166,
            Height = 221,
            CornerRadius = new CornerRadius(13),
            Background = new SolidColorBrush(Color.FromRgb(0xEC, 0xEC, 0xF1)),
            ClipToBounds = true,
        };

        var img = new Image
        {
            Stretch = Stretch.UniformToFill,
            Width = 166,
            Height = 221,
            HorizontalAlignment = HorizontalAlignment.Center,
            VerticalAlignment = VerticalAlignment.Center,
        };
        var ph = new TextBlock
        {
            Text = d.Title.Length > 4 ? d.Title[..4] : d.Title,
            FontSize = 16,
            FontWeight = FontWeights.SemiBold,
            Foreground = new SolidColorBrush(Color.FromRgb(0x9A, 0x9A, 0xA0)),
            HorizontalAlignment = HorizontalAlignment.Center,
            VerticalAlignment = VerticalAlignment.Center,
        };
        var overlay = new Grid();
        overlay.Children.Add(ph);
        overlay.Children.Add(img);
        poster.Child = overlay;
        root.Children.Add(poster);

        var meta = new StackPanel { Margin = new Thickness(10, 10, 10, 4) };
        meta.Children.Add(new TextBlock
        {
            Text = d.Title,
            FontSize = 13,
            FontWeight = FontWeights.SemiBold,
            Foreground = new SolidColorBrush(Color.FromRgb(0x1D, 0x1D, 0x1F)),
            TextTrimming = TextTrimming.CharacterEllipsis,
        });
        meta.Children.Add(new TextBlock
        {
            Text = $"{d.TotalEpisode}集 · {d.Score}",
            FontSize = 11,
            Foreground = new SolidColorBrush(Color.FromRgb(0x6E, 0x6E, 0x73)),
            Margin = new Thickness(0, 3, 0, 0),
        });
        root.Children.Add(meta);
        card.Child = root;

        _ = LoadCoverAsync(img, ph, d.Cover);

        card.MouseLeftButtonUp += (_, _) =>
        {
            var win = new PlayerWindow(Api, d, 1) { Owner = this };
            win.Show();
        };
        return card;
    }

    private async Task LoadCoverAsync(System.Windows.Controls.Image img, TextBlock ph, string cover)
    {
        var url = Api.CoverUrl(cover);
        if (string.IsNullOrEmpty(url)) return;

        if (CoverCache.TryGetValue(url, out var cached))
        {
            img.Source = cached;
            ph.Visibility = Visibility.Collapsed;
            return;
        }

        await CoverGate.WaitAsync();
        try
        {
            if (CoverCache.TryGetValue(url, out cached))
            {
                img.Source = cached;
                ph.Visibility = Visibility.Collapsed;
                return;
            }
            var bytes = await Api.DownloadBytesAsync(url);
            if (bytes == null || bytes.Length == 0) return;

            var bmp = new BitmapImage();
            bmp.BeginInit();
            bmp.CacheOption = BitmapCacheOption.OnLoad;
            bmp.DecodePixelWidth = 252;
            bmp.StreamSource = new MemoryStream(bytes);
            bmp.EndInit();
            bmp.Freeze();
            CoverCache[url] = bmp;

            await Dispatcher.InvokeAsync(() =>
            {
                img.Source = bmp;
                ph.Visibility = Visibility.Collapsed;
            }, DispatcherPriority.Background);
        }
        catch { }
        finally
        {
            CoverGate.Release();
        }
    }

    private async void Login_Click(object sender, RoutedEventArgs e)
    {
        LoginError.Text = "";
        try
        {
            var url = AppConfig.NormalizeServerUrl(ServerBox.Text);
            var user = UserBox.Text.Trim();
            var pass = PassBox.Password;
            if (string.IsNullOrEmpty(url) || string.IsNullOrEmpty(user) || string.IsNullOrEmpty(pass))
            {
                LoginError.Text = "请填写服务器、账号和密码";
                return;
            }
            Api.SetBaseUrl(url);
            await Api.Login(user, pass);
            Config.ServerUrl = url;
            Config.Username = user;
            Config.Password = pass;
            Config.Save();
            ShowMain();
            await LoadLibraryAsync(false);
        }
        catch (Exception ex)
        {
            LoginError.Text = ex.Message;
        }
    }

    private async void PassBox_KeyDown(object sender, System.Windows.Input.KeyEventArgs e)
    {
        if (e.Key == Key.Enter) Login_Click(sender, e);
        await Task.CompletedTask;
    }

    private async void Refresh_Click(object sender, RoutedEventArgs e)
    {
        CoverCache.Clear();
        await LoadLibraryAsync(true);
    }

    private void HideVip_Click(object sender, RoutedEventArgs e)
    {
        _hideVip = !_hideVip;
        Recalculate();
    }

    private void ChipSort_Click(object sender, RoutedEventArgs e)
    {
        _sort = _sort switch { "random" => "latest", "latest" => "score", _ => "random" };
        ChipSort.Content = "排序: " + _sort switch
        {
            "random" => "随机",
            "latest" => "最新",
            _ => "评分",
        };
        Recalculate();
    }

    private void ChipSource_Click(object sender, RoutedEventArgs e)
    {
        if (sender is not Button b || b.Tag is not string tag) return;
        _source = tag;
        void SetSel(Button btn, bool on)
        {
            if (on)
            {
                btn.Background = new LinearGradientBrush(
                    Color.FromRgb(0x4D, 0x9F, 0xFF), Color.FromRgb(0x6C, 0x5C, 0xE7),
                    new Point(0, 0), new Point(1, 1));
                btn.Foreground = Brushes.White;
            }
            else
            {
                btn.Background = new SolidColorBrush(Color.FromRgb(0xEC, 0xEC, 0xF1));
                btn.Foreground = new SolidColorBrush(Color.FromRgb(0x1D, 0x1D, 0x1F));
            }
        }
        SetSel(ChipHongguo, tag == "hongguo");
        SetSel(ChipHuangguo, tag == "huangguo");
        SetSel(ChipHuangdou, tag == "huangdou");
        SetSel(ChipAll, tag == "all");
        Recalculate();
    }

    private async void Search_KeyDown(object sender, System.Windows.Input.KeyEventArgs e)
    {
        if (e.Key != Key.Enter) return;
        var q = SearchBox.Text?.Trim() ?? "";
        if (string.IsNullOrEmpty(q))
        {
            await LoadLibraryAsync(true);
            return;
        }
        try
        {
            var (data, _) = await Api.Search(q);
            _all = data;
            Recalculate();
        }
        catch { }
    }

    private void Nav_Click(object sender, RoutedEventArgs e)
    {
        if (sender is not Button b) return;
        bool isLib = b == NavLibrary;
        LibraryView.Visibility = isLib ? Visibility.Visible : Visibility.Collapsed;
        SettingsView.Visibility = isLib ? Visibility.Collapsed : Visibility.Visible;
        TitleText.Text = isLib ? "短剧库" : "设置";
        var selBg = new SolidColorBrush(Color.FromArgb(0x1F, 0x4D, 0x9F, 0xFF));
        NavLibrary.Tag = isLib ? "selected" : "normal";
        NavSettings.Tag = isLib ? "normal" : "selected";
        NavLibrary.Background = isLib ? selBg : Brushes.Transparent;
        NavSettings.Background = isLib ? Brushes.Transparent : selBg;
    }

    private void Src_Changed(object sender, RoutedEventArgs e)
    {
        Config.ShowHuangguo = ChkHuangguo.IsChecked == true;
        Config.ShowHuangdou = ChkHuangdou.IsChecked == true;
        Config.Save();
        Recalculate();
    }

    private async void Logout_Click(object sender, RoutedEventArgs e)
    {
        try { await Api.Logout(); } catch { }
        MainPanel.Visibility = Visibility.Collapsed;
        LoginPanel.Visibility = Visibility.Visible;
    }

    private void Reconfig_Click(object sender, RoutedEventArgs e)
    {
        ServerBox.Text = Config.ServerUrl;
        UserBox.Text = Config.Username;
        PassBox.Password = Config.Password;
        MainPanel.Visibility = Visibility.Collapsed;
        LoginPanel.Visibility = Visibility.Visible;
    }

    private void TitleBar_MouseLeftButtonDown(object sender, MouseButtonEventArgs e)
    {
        if (e.ClickCount == 2) MaxBtn_Click(sender, e);
        else DragMove();
    }

    private void MinBtn_Click(object sender, RoutedEventArgs e) => WindowState = WindowState.Minimized;

    private void MaxBtn_Click(object sender, RoutedEventArgs e)
        => WindowState = WindowState == WindowState.Maximized ? WindowState.Normal : WindowState.Maximized;

    private void CloseBtn_Click(object sender, RoutedEventArgs e) => Close();
}
