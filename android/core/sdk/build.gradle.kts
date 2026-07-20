plugins {
    id("opcode42.android.library")
    id("opcode42.android.hilt")
    alias(libs.plugins.kotlin.serialization)
}

android {
    namespace = "dev.opcode42.core.sdk"
}

dependencies {
    api(project(":core:model"))
    implementation(libs.okhttp)
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.kotlinx.coroutines.android)

    testImplementation(libs.junit)
    testImplementation(libs.kotlinx.coroutines.test)
    testImplementation(libs.kotlinx.serialization.json)
    testImplementation(libs.okhttp)
    testImplementation(libs.okhttp.mockwebserver)
}
