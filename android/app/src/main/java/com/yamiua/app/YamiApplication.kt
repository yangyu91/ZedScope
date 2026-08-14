package com.yamiua.app

import android.app.Application
import android.content.Context
import android.util.Log
import java.io.File
import yami.Yami

/**
 * gomobile 启动入口。
 *
 * 必须在最早、最安全的时机（Application.onCreate，早于任何 Activity）用
 * Application Context 初始化 Go runtime。缺这一步，第一次调用 Yami.* 触发
 * .so 加载与 Go runtime 初始化时，会在部分 Android 版本上因 context 未就绪
 * 而 native 崩溃（SIGSEGV / SIGABRT）—— 表现为"点开就闪退"，且被 Kotlin
 * try/catch 完全拦不住。这极可能是历史版本一直打不开的根因。
 */
class YamiApplication : Application() {
    override fun onCreate() {
        super.onCreate()
        initGoRuntime()
        installCrashHandler()
    }

    /**
     * 反射调用 gomobile 生成的 Yami.init(Context)：Go runtime 在 Android 上最早、
     * 最安全的初始化点。反射方式避免对生成方法签名/存在性的编译期依赖——
     * 若存在则正确初始化，若不存在则静默跳过，回退到 gomobile 的自动 init。
     */
    private fun initGoRuntime() {
        try {
            val m = Yami::class.java.getMethod("init", Context::class.java)
            m.invoke(null, applicationContext)
            Log.i("Yami", "Yami.init() ok")
        } catch (e: Throwable) {
            Log.e("Yami", "Yami.init unavailable, relying on gomobile auto-init", e)
        }
    }

    /**
     * 全局未捕获异常处理：把 Kotlin 层堆栈写入 cacheDir/crash.log。
     * 注意 native 崩溃（Go panic -> SIGABRT）Kotlin 层抓不到，但 Kotlin 异常
     * 能落盘，便于不带电脑也能回传真实崩溃原因。
     */
    private fun installCrashHandler() {
        val def = Thread.getDefaultUncaughtExceptionHandler()
        Thread.setDefaultUncaughtExceptionHandler { t, e ->
            try {
                val f = File(cacheDir, "crash.log")
                f.appendText("${System.currentTimeMillis()} thread=${t.name}\n${Log.getStackTraceString(e)}\n----\n")
            } catch (_: Throwable) {
                // best-effort
            }
            def?.uncaughtException(t, e)
        }
    }
}
