plugins {
    alias(libs.plugins.kotlin.multiplatform)
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.kapt)
    alias(libs.plugins.hilt)
}

kotlin {
    androidTarget {
        compilations.all {
            compilerOptions.configure { jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17) }
        }
    }

    sourceSets {
        commonMain.dependencies {
            api(project(":core:model"))
            api(project(":core:store"))
            implementation(libs.kotlinx.coroutines.core)
        }
        commonTest.dependencies {
            implementation(kotlin("test"))
        }
        androidMain.dependencies {
            implementation(libs.okhttp)
            implementation(libs.okhttp.sse)
            implementation(libs.okhttp.logging)
            implementation(libs.kotlinx.coroutines.android)
            implementation(libs.android.lifecycle.process)
            implementation(libs.hilt.android)
        }
        androidUnitTest.dependencies {
            implementation(kotlin("test"))
            implementation(libs.okhttp.mockwebserver)
            implementation(libs.kotlinx.coroutines.core)
        }
    }
}

android {
    namespace = "dev.opcode42.core.network"
    compileSdk = 35
    defaultConfig { minSdk = 26 }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    testOptions {
        // SseManager logs via android.util.Log; in local JVM unit tests the android.jar stubs
        // throw unless we let them return defaults. The test exercises the coroutine/SSE logic,
        // not logging, so default (no-op) stubs are exactly what we want.
        unitTests.isReturnDefaultValues = true
    }
}

dependencies {
    add("kapt", libs.hilt.android.compiler)
}
