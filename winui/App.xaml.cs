using Microsoft.UI.Xaml;

namespace JukuWinUI;

public partial class App : Application
{
    public static Window? MainWindow { get; private set; }

    public App()
    {
        InitializeComponent();
        var timer = new DispatcherTimer { Interval = TimeSpan.FromSeconds(2) };
        timer.Tick += (_, _) => { try { System.IO.File.AppendAllText(System.IO.Path.Combine(AppContext.BaseDirectory, "juku-err.log"), "[tick]" + DateTime.Now.ToString("HH:mm:ss") + Environment.NewLine); } catch {} };
        timer.Start();
        UnhandledException += (_, e) =>
        {
            try { System.IO.File.AppendAllText(System.IO.Path.Combine(AppContext.BaseDirectory, "juku-err.log"), e.Exception + Environment.NewLine); } catch {}
        };
    }

    protected override void OnLaunched(LaunchActivatedEventArgs args)
    {
        MainWindow = new MainWindow();
        MainWindow.Activate();
    }
}
