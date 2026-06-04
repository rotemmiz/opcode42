// Forge Kotlin SDK — the generated REST client (gen/) plus the hand-written
// ForgeClient wrapper (src/). Regenerate gen/ with `scripts/gen-sdks.sh`
// (`make gen-sdks`); never edit gen/ by hand.
//
// This is a plain Kotlin/JVM library so it builds in CI without the Android
// toolchain. The Android app (plan 07) currently ships its own hand-written
// client in core:sdk; it can migrate onto this generated SDK when ready.

plugins {
    kotlin("jvm") version "1.9.23"
    kotlin("plugin.serialization") version "1.9.23"
}

repositories {
    mavenCentral()
}

sourceSets {
    main {
        kotlin.srcDirs("gen/src/main/kotlin", "src/main/kotlin")
    }
}

dependencies {
    implementation("org.jetbrains.kotlin:kotlin-stdlib-jdk8")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-core:1.8.0")
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.6.3")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
}

kotlin {
    compilerOptions {
        freeCompilerArgs.add("-opt-in=kotlinx.serialization.ExperimentalSerializationApi")
    }
}
