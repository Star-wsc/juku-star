using JukuWinUI.Core;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media.Imaging;
using Microsoft.UI.Xaml.Navigation;

namespace JukuWinUI.Views;

public sealed partial class DetailPage : Page
{
    private Drama _drama = new();

    public DetailPage()
    {
        InitializeComponent();
    }

    protected override void OnNavigatedTo(NavigationEventArgs e)
    {
        base.OnNavigatedTo(e);
        if (e.Parameter is Drama d)
        {
            _drama = d;
            TitleText.Text = d.Title;
            MetaText.Text = $"{d.Category} · {d.TotalEpisode} 集 · {d.Score} 分" + (d.Vip ? " · VIP" : "");
            DescText.Text = d.Desc;
            var url = MainWindow.Api.CoverUrl(d.Cover);
            if (!string.IsNullOrEmpty(url))
            {
                try { CoverImage.Source = new BitmapImage(new Uri(url)); } catch { }
            }
            EpisodeList.Items.Clear();
            var count = Math.Min(d.TotalEpisode, 12);
            for (var i = 1; i <= count; i++)
            {
                var ep = i;
                var btn = new Button
                {
                    Content = $"第{ep:00}集",
                    Style = (Style)Application.Current.Resources["ChipStyle"],
                    Tag = ep,
                };
                btn.Click += async (_, _) => await PlayAsync(ep);
                EpisodeList.Items.Add(btn);
            }
        }
    }

    private void BackBtn_Click(object sender, RoutedEventArgs e)
    {
        if (Frame.CanGoBack) Frame.GoBack();
    }

    private async void PlayBtn_Click(object sender, RoutedEventArgs e) => await PlayAsync(1);

    private async Task PlayAsync(int ep)
    {
        try
        {
            await MainWindow.Api.PlaybackOpen(_drama.Id, true);
            Frame.Navigate(typeof(PlayerPage), (_drama, ep));
        }
        catch { }
    }
}

public sealed partial class PlaceholderPage : Page
{
    public PlaceholderPage()
    {
        InitializeComponent();
    }
}
