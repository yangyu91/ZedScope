package com.zedscope.app

import android.app.Application
import android.util.Log
import java.io.File

/**
 * ZedScope Application 入口。
 *
 * 职责（仅保留诊断能力，不再手动初始化 Go runtime）：
 *   - 安装全局未捕获异常处理器：Kotlin 层崩溃写入 cacheDir/crash.log。
 *   - 把启动阶段写入 cacheDir/boot.log，闪退前最后一行即死因线索，
 *     下一 launch 由 MainActivity 回显，无需 adb 即可定位。
 *
 * 为什么不再反射调用 Yami.init(Context)：
 *   实测（Android 14）在 Application.onCreate 早期反射调用 gomobile 的
 *   Yami.init 会触发 native 初始化路径差异，导致"打开即闪退"(SIGSEGV/SIGABRT)，
 *   Kotlin try/catch 拦不住。gomobile 的 Go runtime 本身会在首次调用任意
 *   Yami.* 原生方法时自动懒初始化（v0.2 即此路径，可用），因此这里不主动调。
 */
class YamiApplication : Application() {
    override fun onCreate() {
        super.onCreate()
        boot("Application.onCreate start")
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
