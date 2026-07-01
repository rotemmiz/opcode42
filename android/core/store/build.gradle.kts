plugins {
    alias(libs.plugins.kotlin.multiplatform)
    alias(libs.plugins.android.library)
    alias(libs.plugins.ksp)
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
            api(libs.kotlinx.coroutines.core)
        }
        commonTest.dependencies {
            implementation(kotlin("test"))
        }
        androidMain.dependencies {
            implementation(libs.hilt.android)
        }
    }
}

android {
    namespace = "dev.opcode42.core.store"
    compileSdk = 35
    defaultConfig { minSdk = 26 }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    add("kspAndroid", libs.hilt.android.compiler)
}
