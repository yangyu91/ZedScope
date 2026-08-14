pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    // Optional mirror for restricted networks: set YAMI_MAVEN_MIRROR=1 to use
    // Tencent mirrors when google()/mavenCentral() are unreachable. The default
    // path keeps google()/mavenCentral() (works on GitHub Actions and normal dev
    // machines).
    val useMirror = System.getenv("YAMI_MAVEN_MIRROR") != null
    repositories {
        if (useMirror) {
            maven { url = uri("https://mirrors.tencent.com/nexus/repository/maven-public/") }
        }
        google()
        mavenCentral()
    }
}

rootProject.name = "ZedScope"
include(":app")
