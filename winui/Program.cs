using System.Runtime.InteropServices;
using Microsoft.UI.Xaml;

namespace JukuWinUI;

public static class Program
{
    [DllImport("Microsoft.WindowsAppRuntime.Bootstrap.dll", ExactSpelling = true)]
    private static extern int MddBootstrapInitialize(uint majorMinorVersion, string versionTag, ulong packageVersion, int options);

    [STAThread]
    public static void Main(string[] args)
    {
        Log("Main enter");
        try
        {
            WinRT.ComWrappersSupport.InitializeComWrappers();
            Log("ComWrappers ok");
        }
        catch (Exception ex) { Log("cw ex " + ex.Message); }

        try
        {
            int hr = 0; // bootstrap disabled; system runtime installed
            Log("bootstrap hr=0x" + hr.ToString("X8"));
        }
        catch (Exception ex) { Log("boot ex " + ex.Message); }

        try
        {
            Log("calling Application.Start");
            Application.Start(_ =>
            {
                Log("Start callback");
                new App();
                Log("App created");
            });
            Log("Start returned");
        }
        catch (Exception ex)
        {
            Log("Start ex " + ex.GetType().Name + " " + ex.Message);
        }
    }

    private static void Log(string msg)
    {
        try
        {
            File.AppendAllText(
                Path.Combine(AppContext.BaseDirectory, "juku-start.log"),
                DateTime.Now.ToString("HH:mm:ss.fff") + " " + msg + "\n");
        }
        catch { }
    }
}
