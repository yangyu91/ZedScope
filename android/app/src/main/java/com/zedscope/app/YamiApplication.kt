package com.zedscope.app

import android.app.Application
import android.content.Context
import android.util.Log
import java.io.File
import yami.Yami

/**
 * gomobile 启动入口。
 *
 * 注意：本项目的 gomobile 绑定（x/mobile@latest + go 1.25 生成）并不会暴露一个
 * 可被直接调用的 `Yami.init(Context)` 静态方法（直接调用会在编译期报错），Go runtime
 * 在首次调用任意 `Yami.*` 原生方法时由 gomobile 自动懒初始化。因此这里用反射"尽力而为"
 * 地尝试显式 init：若绑定恰好提供了该方法就调它；若没有（本项目当前版本）则静默回退到
 * gomobile 的自动初始化，不会因此引入新的崩溃。
 *
 * 同时，本类把启动轨迹写入 cacheDir/boot.log（每次追加并落盘），便于"点开就闪退"时在
 * 用户侧留下最后到达的阶段——下一 launch 会在 MainActivity 里把最后阶段回显出来，
 * 无需 adb 即可定位崩溃发生在 Application 还是 MainActivity 的哪一步。
 */
class YamiApplication : Application() {
    override fun onCreate() {
        super.onCreate()
        boot("Application.onCreate start")
        initGoRuntime()
        installCrashHandler()
        boot("Application.onCreate done")
    }

    /** 把启动阶段追加写入 cacheDir/boot.log（每次打开即落盘，闪退前最后一行即死因线索）。 */
    private fun boot(stage: String) {
        try {
            File(cacheDir, "boot.log").appendText("${System.currentTimeMillis()} $stage\n")
        } catch (_: Throwable) {
            // best-effort
        }
    }

    /**
     * 尽力而为地显式初始化 Go runtime（反射调用可能的 Yami.init(Context)）。
     * 该方法在本项目当前 gomobile 版本下通常不存在，此时静默回退到自动初始化。
     */
    private fun initGoRuntime() {
        try {
            val m = Yami::class.java.getMethod("init", Context::class.java)
            m.invoke(null, applicationContext)
            Log.i("Yami", "Yami.init() ok")
            boot("Yami.init ok")
        } catch (e: Throwable) {
            Log.e("Yami", "Yami.init unavailable, relying on gomobile auto-init", e)
            boot("Yami.init unavailable: ${e.message}")
        }
    }

    /**
     * 全局未捕获异常处理：把 Kotlin 层堆栈写入 cacheDir/crash.log，并在 boot.log 留痕。
     * 注意 native 崩溃（Go panic -> SIGABRT）Kotlin 层抓不到，但 Kotlin 异常能落盘，
     * 便于不带电脑也能回传真实崩溃原因。
     */
    private fun installCrashHandler() {
        val def = Thread.getDefaultUncaughtExceptionHandler()
        Thread.setDefaultUncaughtExceptionHandler { t, e ->
            try {
                File(cacheDir, "boot.log").appendText("${System.currentTimeMillis()} CRASH thread=${t.name}\n")
                File(cacheDir, "crash.log").appendText("${System.currentTimeMillis()} thread=${t.name}\n${Log.getStackTraceString(e)}\n----\n")
            } catch (_: Throwable) {
                // best-effort
            }
            def?.uncaughtException(t, e)
        }
    }
}
