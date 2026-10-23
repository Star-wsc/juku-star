using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace JukuWinUI.Views;

public sealed partial class SettingsPage : Page
{
    public SettingsPage()
    {
        InitializeComponent();
        Loaded += (_, _) =>
        {
            var cfg = MainWindow.Config;
            UserText.Text = MainWindow.AccountName;
            ServerText.Text = cfg.ServerUrl;
            SwHuangguo.IsOn = cfg.ShowHuangguo;
            SwHuangdou.IsOn = cfg.ShowHuangdou;
            SwAutoNext.IsOn = cfg.AutoNext;
            SwRandom.IsOn = cfg.Sort == "random";
        };
    }

    private void SwHuangguo_Toggled(object sender, RoutedEventArgs e)
    {
        MainWindow.Config.ShowHuangguo = SwHuangguo.IsOn;
        MainWindow.Config.Save();
    }

    private void SwHuangdou_Toggled(object sender, RoutedEventArgs e)
    {
        MainWindow.Config.ShowHuangdou = SwHuangdou.IsOn;
        MainWindow.Config.Save();
    }

    private void SwAutoNext_Toggled(object sender, RoutedEventArgs e)
    {
        MainWindow.Config.AutoNext = SwAutoNext.IsOn;
        MainWindow.Config.Save();
    }

    private void SwRandom_Toggled(object sender, RoutedEventArgs e)
    {
        MainWindow.Config.Sort = SwRandom.IsOn ? "random" : "latest";
        MainWindow.Config.Save();
    }

    private async void LogoutBtn_Click(object sender, RoutedEventArgs e)
    {
        try { await MainWindow.Api.Logout(); } catch { }
        if (App.MainWindow is MainWindow mw) mw.LogoutToLogin();
    }

    private void ReconfigBtn_Click(object sender, RoutedEventArgs e)
    {
        if (App.MainWindow is MainWindow mw) mw.LogoutToLogin();
    }
}
