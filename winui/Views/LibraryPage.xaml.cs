using System.IO;
using System.Runtime.InteropServices.WindowsRuntime;
using JukuWinUI.Core;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media.Imaging;
using Microsoft.UI.Xaml.Media;

namespace JukuWinUI.Views;

public sealed class DramaItem
{
    public Drama Drama { get; init; } = new();
    public string Title => Drama.Title;
    public string CoverUrl { get; init; } = "";
    public string Meta { get; init; } = "";
}

public sealed partial class LibraryPage : Page
{
    private static void LogX(string m)
    {
        try { System.IO.File.AppendAllText(System.IO.Path.Combine(AppContext.BaseDirectory, "juku-err.log"), "[lib] " + DateTime.Now.ToString("HH:mm:ss.fff") + " " + m + Environment.NewLine); } catch {}
    }

    private List<Drama> _all = new();
    private string _source = "hongguo";
    private string _sort = "random";
    private bool _hideVip;

    public LibraryPage()
    {
        LogX("ctor");
        InitializeComponent();
        LogX("init ok");
        Loaded += async (_, _) => await OnLoaded();
    }

    private async Task OnLoaded()
    {
        LogX("OnLoaded");
        var cfg = MainWindow.Config;
        _sort = cfg.Sort;
        _hideVip = cfg.HideVip;
        _source = string.IsNullOrEmpty(cfg.SelectedSource) ? "hongguo" : cfg.SelectedSource;
        ChipHuangguo.Visibility = cfg.ShowHuangguo ? Visibility.Visible : Visibility.Collapsed;
        ChipHuangdou.Visibility = cfg.ShowHuangdou ? Visibility.Visible : Visibility.Collapsed;
        UpdateChips();
        await LoadAsync(false);
    }

    private async Task LoadAsync(bool refresh)
    {
        LogX("LoadAsync " + refresh);
        RefreshBtn.IsEnabled = false;
        try
        {
            LogX("fetch begin");
            var res = await MainWindow.Api.FetchDramas(refresh);
            LogX("fetch ok data=" + (res?.Data?.Count ?? -1));
            _all = res.Data ?? new();
            LogX("fetched " + _all.Count);
            // await 之后必须回 UI 线程，否则 COMException / RPC_E_WRONG_THREAD
            if (DispatcherQueue.HasThreadAccess) ApplyFilter();
            else DispatcherQueue.TryEnqueue(() => { try { ApplyFilter(); } catch (Exception ex2) { LogX("af " + ex2.Message); } });
        }
        catch (Exception ex)
        {
            LogX("LoadAsync ex " + ex.GetType().Name + " " + ex.Message);
            try { SubText.Text = "加载失败：" + ex.Message; } catch {}
        }
        finally
        {
            // finally 可能跑在线程池，UI 属性必须回主线程
            DispatcherQueue.TryEnqueue(() => { RefreshBtn.IsEnabled = true; });
        }
    }

    private void ApplyFilter()
    {
        var cfg = MainWindow.Config;
        IEnumerable<Drama> list = _all.Where(d => !cfg.IsSourceHidden(d.Source));
        if (_source != "all")
            list = list.Where(d => AppConfig.SourceGroup(d.Source) == _source || d.Source == _source);
        if (_hideVip)
            list = list.Where(d => !d.Vip);
        var arr = list.ToList();
        arr = _sort switch
        {
            "random" => arr.OrderBy(_ => Random.Shared.Next()).ToList(),
            "latest" => arr.OrderByDescending(d => d.OnlineDate).ToList(),
            "score" => arr.OrderByDescending(d => double.TryParse(d.Score, out var s) ? s : 0).ToList(),
            _ => arr,
        };
        DramaGrid.Children.Clear();
        foreach (var d in arr.Take(40))
        {
            try { DramaGrid.Children.Add(BuildCard(d)); }
            catch (Exception ex) { LogX("card ex " + ex.Message); }
        }
        LogX("cards added " + Math.Min(40, arr.Count));
        SubText.Text = $"发现更多好看的故事 · 共 {arr.Count} 部";
    }

    private void UpdateChips()
    {
        void Set(Button b, bool selected)
            => b.Style = (Style)(selected ? Application.Current.Resources["ChipSelectedStyle"]
                                          : Application.Current.Resources["ChipStyle"]);
        Set(ChipHongguo, _source == "hongguo");
        Set(ChipHuangguo, _source == "huangguo");
        Set(ChipHuangdou, _source == "huangdou");
        Set(ChipAll, _source == "all");
        Set(ChipHideVip, _hideVip);
        ChipSort.Content = "排序: " + (_sort switch
        {
            "random" => "随机",
            "latest" => "最新上线",
            "score" => "评分",
            _ => "默认",
        });
    }

    private UIElement BuildCard(Drama d)
    {
        var sp = new StackPanel { Width = 168, Margin = new Thickness(0, 0, 16, 16) };
        var border = new Border
        {
            Width = 168,
            Height = 224,
            CornerRadius = new CornerRadius(14),
            Background = new SolidColorBrush(Microsoft.UI.Colors.LightGray),
        };
        var g = new Grid();
        g.Children.Add(new TextBlock
        {
            Text = d.Title,
            Margin = new Thickness(12),
            TextWrapping = TextWrapping.Wrap,
            FontSize = 14,
            Foreground = new SolidColorBrush(Microsoft.UI.Colors.DarkSlateGray),
            HorizontalAlignment = HorizontalAlignment.Center,
            VerticalAlignment = VerticalAlignment.Center,
        });
        var img = new Image { Stretch = Stretch.UniformToFill };
        g.Children.Add(img);
        border.Child = g;
        sp.Children.Add(border);
        sp.Children.Add(new TextBlock
        {
            Text = d.Title,
            FontSize = 13,
            FontWeight = Microsoft.UI.Text.FontWeights.SemiBold,
            TextTrimming = TextTrimming.CharacterEllipsis,
        });
        sp.Children.Add(new TextBlock
        {
            Text = d.TotalEpisode + "集 · " + d.Score,
            FontSize = 11,
            Foreground = new SolidColorBrush(Microsoft.UI.Colors.Gray),
            Margin = new Thickness(0, 4, 0, 0),
        });
        var btn = new Button
        {
            Content = sp,
            Padding = new Thickness(0),
            Background = new SolidColorBrush(Microsoft.UI.Colors.Transparent),
            BorderThickness = new Thickness(0),
            HorizontalContentAlignment = HorizontalAlignment.Left,
        };
        btn.Click += (_, _) => Frame.Navigate(typeof(DetailPage), d);
        _ = LoadCover(img, d.Cover);
        return btn;
    }

    private async Task LoadCover(Image img, string cover)
    {
        try
        {
            var url = MainWindow.Api.CoverUrl(cover);
            if (string.IsNullOrEmpty(url)) return;
            var bytes = await MainWindow.Api.DownloadBytesAsync(url);
            if (bytes is not { Length: > 100 }) return;
            var data = bytes;
            DispatcherQueue.TryEnqueue(async () =>
            {
                try
                {
                    var bmp = new BitmapImage();
                    using var ms = new MemoryStream(data);
                    await bmp.SetSourceAsync(ms.AsRandomAccessStream());
                    img.Source = bmp;
                }
                catch (Exception ex) { LogX("cover set ex " + ex.Message); }
            });
        }
        catch (Exception ex) { LogX("cover dl ex " + ex.Message); }
    }

    private void ChipSource_Click(object sender, RoutedEventArgs e)
    {
        if (sender is Button b && b.Tag is string tag)
        {
            _source = tag;
            MainWindow.Config.SelectedSource = tag;
            MainWindow.Config.Save();
            UpdateChips();
            ApplyFilter();
        }
    }

    private void ChipSort_Click(object sender, RoutedEventArgs e)
    {
        _sort = _sort switch
        {
            "random" => "latest",
            "latest" => "score",
            "score" => "default",
            _ => "random",
        };
        MainWindow.Config.Sort = _sort;
        MainWindow.Config.Save();
        UpdateChips();
        ApplyFilter();
    }

    private void ChipHideVip_Click(object sender, RoutedEventArgs e)
    {
        _hideVip = !_hideVip;
        MainWindow.Config.HideVip = _hideVip;
        MainWindow.Config.Save();
        UpdateChips();
        ApplyFilter();
    }

    private async void RefreshBtn_Click(object sender, RoutedEventArgs e) => await LoadAsync(true);

    private async void SearchBox_QuerySubmitted(AutoSuggestBox sender, AutoSuggestBoxQuerySubmittedEventArgs args)
    {
        var q = sender.Text?.Trim() ?? "";
        if (string.IsNullOrEmpty(q))
        {
            await LoadAsync(true);
            return;
        }
        try
        {
            var (data, _) = await MainWindow.Api.Search(q);
            _all = data;
            ApplyFilter();
        }
        catch { }
    }

    private void DramaGrid_ItemClick(object sender, ItemClickEventArgs e)
    {
        if (e.ClickedItem is DramaItem item)
            Frame.Navigate(typeof(DetailPage), item.Drama);
    }

    private async void DramaGrid_ContainerContentChanging(ListViewBase sender, ContainerContentChangingEventArgs args)
    {
        if (args.Item is not DramaItem item) return;
        if (args.ItemContainer is not GridViewItem gvi) return;
        if (gvi.ContentTemplateRoot is not StackPanel sp) return;
        foreach (var child in sp.Children)
        {
            if (child is Border border && border.Child is Grid grid)
            {
                foreach (var g in grid.Children)
                {
                    if (g is Image img)
                    {
                        if (string.IsNullOrEmpty(item.CoverUrl))
                        {
                            img.Source = null;
                        }
                        else
                        {
                            try {
                                var bytes = await MainWindow.Api.DownloadBytesAsync(item.CoverUrl);
                                if (bytes is { Length: > 100 })
                                {
                                    var data = bytes;
                                    DispatcherQueue.TryEnqueue(async () =>
                                    {
                                        try
                                        {
                                            var bmp = new BitmapImage();
                                            using var ms = new MemoryStream(data);
                                            await bmp.SetSourceAsync(ms.AsRandomAccessStream());
                                            img.Source = bmp;
                                            if (img.Parent is Grid g)
                                            {
                                                foreach (UIElement el in g.Children)
                                                    if (el is TextBlock tb) tb.Visibility = Visibility.Collapsed;
                                            }
                                        }
                                        catch (Exception ex2) { LogX("img set ex " + ex2.Message); }
                                    });
                                }
                            } catch (Exception ex) { LogX("img dl ex " + ex.Message); }
                        }
                    }
                }
            }
        }
    }

    private void CoverImage_ImageOpened(object sender, RoutedEventArgs e)
    {
        if (sender is Image img && img.Parent is Grid g)
        {
            foreach (var child in g.Children)
            {
                if (child is TextBlock tb && tb.Name == "Placeholder")
                    tb.Visibility = Visibility.Collapsed;
            }
        }
    }
}
