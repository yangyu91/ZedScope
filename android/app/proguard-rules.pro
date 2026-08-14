# ZedScope proguard rules
# Keep the gomobile binding entry points (called via JNI from Kotlin).
-keep class yami.Yami { *; }
-keep class com.zedscope.app.YamiCore { *; }
-keep class com.zedscope.app.Token { *; }
-keep class com.zedscope.app.Record { *; }

-dontwarn org.bouncycastle.**
-dontwarn org.conscrypt.**
