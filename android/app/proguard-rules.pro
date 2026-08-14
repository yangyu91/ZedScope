# yami-UA proguard rules
# Keep the gomobile binding entry points (called via JNI from Kotlin).
-keep class yami.Yami { *; }
-keep class com.yamiua.app.YamiCore { *; }
-keep class com.yamiua.app.Token { *; }
-keep class com.yamiua.app.Record { *; }

-dontwarn org.bouncycastle.**
-dontwarn org.conscrypt.**
