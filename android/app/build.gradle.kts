plugins {
    id("com.android.application")
    id("kotlin-android")
}

android {
    namespace = "com.zedscope.app"
    compileSdk = 34

    defaultConfig {
        applicationId = "com.zedscope.app"
        minSdk = 24
        targetSdk = 34
        versionCode = 102
        versionName = "1.2.0"
    }

    // Signing: prefer a real keystore from the environment; fall back to the
    // Android debug keystore so `assembleRelease` always produces an
    // installable APK without manual keystore setup.
    signingConfigs {
        create("release") {
            val keystoreFile = System.getenv("KEYSTORE_FILE")
            if (keystoreFile != null && File(keystoreFile).exists()) {
                storeFile = file(keystoreFile)
                storePassword = System.getenv("KEYSTORE_PASSWORD")
                keyAlias = System.getenv("KEY_ALIAS")
                keyPassword = System.getenv("KEY_PASSWORD")
            } else {
                // AGP default debug keystore (~/.android/debug.keystore).
                val dbg = File(System.getProperty("user.home"), ".android/debug.keystore")
                storeFile = dbg
                storePassword = "android"
                keyAlias = "androiddebugkey"
                keyPassword = "android"
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            signingConfig = signingConfigs.getByName("release")
        }
    }

    buildFeatures {
        viewBinding = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }
}

dependencies {
    // The Go capture engine, produced by `gomobile bind` (see build_android.sh).
    implementation(files("libs/yami.aar"))

    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.appcompat:appcompat:1.7.0")
    implementation("com.google.android.material:material:1.12.0")
    implementation("androidx.constraintlayout:constraintlayout:2.1.4")
    implementation("androidx.recyclerview:recyclerview:1.3.2")
    implementation("androidx.swiperefreshlayout:swiperefreshlayout:1.1.0")
    implementation("androidx.webkit:webkit:1.11.0")
    implementation("com.google.code.gson:gson:2.11.0")
}
