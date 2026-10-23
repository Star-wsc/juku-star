using JukuWinUI.Core;
using JukuWinUI.Views;
using Microsoft.UI.Windowing;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Input;
using Windows.System;

namespace JukuWinUI;

public sealed partial class MainWindow : Window
{
    private static void LogX(string m)
    {
        try { System.IO.File.AppendAllText(System.IO.Path.Combine(AppContext.BaseDirectory, "juku-err.log"), DateTime.Now.ToString("HH:mm:ss.fff") + " " + m + Environment.NewLine); } catch {}
    }

    public static JukuApi Api { get; } = new();
    public static AppConfig Config { get; set; } = new();
    public static string AccountName { get; set; } = "";

    private AppWindow? _appWindow;

    public MainWindow()
    {
        InitializeComponent();
        ExtendsContentIntoTitleBar = true;
        SetTitleBar(TitleBarGrid);

        _appWindow = GetAppWindow();
        if (_appWindow != null)
        {
            _appWindow.Resize(new Windows.Graphics.SizeInt32(1280, 800));
            try { _appWindow.SetIcon("Assets/appicon.ico"); } catch { }
        }

        _ = InitAsync();
    }

    private AppWindow? GetAppWindow()
    {
        var hwnd = WinRT.Interop.WindowNative.GetWindowHandle(this);
        var id = Microsoft.UI.Win32Interop.GetWindowIdFromWindow(hwnd);
        return AppWindow.GetFromWindowId(id);
    }

    private async Task InitAsync()
    {
        LogX("InitAsync");
        Config = AppConfig.Load();
        LogX($"creds={Config.HasCredentials} server={Config.ServerUrl}");
        if (!Config.HasCredentials)
        {
            ShowLogin();
            return;
        }
        // 有保存的账号：静默登录，成功/失败都进主界面，不再反复要求登录
        Api.SetBaseUrl(AppConfig.NormalizeServerUrl(Config.ServerUrl));
        AccountName = Config.Username;
        try
        {
            LogX("login...");
            var account = await Api.Login(Config.Username, Config.Password).WaitAsync(TimeSpan.FromSeconds(12));
            LogX("login ok " + (account?.Username ?? "null"));
            if (!string.IsNullOrEmpty(account?.Username)) AccountName = account.Username;
        }
        catch
        {
            try { await Api.EnsureViewer(); } catch { }
        }
        LogX("ShowMain");
        ShowMain();
    }

    private void ShowLogin()
    {
        if (!DispatcherQueue.HasThreadAccess)
        {
            DispatcherQueue.TryEnqueue(() => ShowLogin());
            return;
        }
        LoginRoot.Visibility = Visibility.Visible;
        MainRoot.Visibility = Visibility.Collapsed;
        TitleText.Text = "短剧库 · 连接服务器";
        ServerBox.Text = Config.ServerUrl;
        UserBox.Text = Config.Username;
        PassBox.Password = Config.Password;
        LoginError.Text = "";
    }

    private void ShowMain()
    {
      try {
        LogX("ShowMain enter");
        // UI 更新必须回到主线程，否则 WinRT 抛 RPC_E_WRONG_THREAD
        if (!DispatcherQueue.HasThreadAccess)
        {
            DispatcherQueue.TryEnqueue(() => ShowMainCore());
            return;
        }
        ShowMainCore();
      } catch (Exception ex) { LogX("ShowMain ex " + ex); }
    }

    private void ShowMainCore()
    {
      try {
        LoginRoot.Visibility = Visibility.Collapsed;
        LogX("login hidden");
        MainRoot.Visibility = Visibility.Visible;
        LogX("main shown");
        TitleText.Text = "短剧库";
        ServerLabel.Text = Config.ServerUrl.Replace("http://", "").Replace("https://", "");
        AccountLabel.Text = AccountName;
        if (NavList.Items.Count > 0 && NavList.SelectedIndex < 0)
            NavList.SelectedIndex = 0;
        LogX("ShowMain->Navigate library");
        NavigateTo("library");
        LogX("Navigate done");
      } catch (Exception ex) { LogX("ShowMain ex " + ex); }
    }

    private void NavigateTo(string tag)
    {
        LogX("NavigateTo " + tag);
        try {
        ContentFrame.Navigate(tag switch
        {
            "settings" => typeof(SettingsPage),
            "following" => typeof(PlaceholderPage),
            "download" => typeof(PlaceholderPage),
            "history" => typeof(PlaceholderPage),
            _ => typeof(LibraryPage),
        }, tag);
        } catch (Exception ex) { LogX("Navigate ex " + ex); }
        TitleText.Text = tag switch
        {
            "settings" => "设置",
            "following" => "追剧",
            "download" => "下载",
            "history" => "历史",
            _ => "短剧库",
        };
    }

    private void NavList_ItemClick(object sender, ItemClickEventArgs e)
    {
        if (e.ClickedItem is ListViewItem item && item.Tag is string tag)
            NavigateTo(tag);
    }

    private async void LoginBtn_Click(object sender, RoutedEventArgs e) => await DoLoginAsync();

    private async void PassBox_KeyDown(object sender, KeyRoutedEventArgs e)
    {
        if (e.Key == VirtualKey.Enter)
        {
            e.Handled = true;
            await DoLoginAsync();
        }
    }

    private async Task DoLoginAsync()
    {
        LoginBtn.IsEnabled = false;
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
            var account = await Api.Login(user, pass);
            AccountName = account.Username;
            Config.ServerUrl = url;
            Config.Username = user;
            Config.Password = pass;
            Config.Save();
            ShowMain();
        }
        catch (Exception ex)
        {
            LoginError.Text = ex.Message;
        }
        finally
        {
            LoginBtn.IsEnabled = true;
        }
    }

    
    public void Minimize()
    {
        if (_appWindow?.Presenter is OverlappedPresenter p) p.Minimize();
    }

    public void ToggleFullscreen()
    {
        if (_appWindow == null) return;
        if (_appWindow.Presenter is OverlappedPresenter p)
        {
            if (p.State == OverlappedPresenterState.Maximized) p.Restore();
            else p.Maximize();
        }
    }

    public void LogoutToLogin() => ShowLogin();

    private void MinBtn_Click(object sender, RoutedEventArgs e)
    {
        if (_appWindow?.Presenter is OverlappedPresenter p) p.Minimize();
    }

    private void MaxBtn_Click(object sender, RoutedEventArgs e)
    {
        if (_appWindow?.Presenter is not OverlappedPresenter p) return;
        if (p.State == OverlappedPresenterState.Maximized)
        {
            p.Restore();
            MaxIcon.Glyph = "";
        }
        else
        {
            p.Maximize();
            MaxIcon.Glyph = "";
        }
    }

    private void CloseBtn_Click(object sender, RoutedEventArgs e) => Close();
}
