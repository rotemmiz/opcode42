plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.kapt)
    alias(libs.plugins.hilt)
}

android {
    namespace = "dev.forge.feature.notifications"
    compileSdk = 35
    defaultConfig { minSdk = 26 }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
    testOptions {
        // PushRegistrar logs via android.util.Log; return defaults so JVM unit
        // tests don't hit the un-mocked-method RuntimeException.
        unitTests.isReturnDefaultValues = true
    }
}

dependencies {
    implementation(project(":core:sdk"))

    implementation(libs.android.core.ktx)
    implementation(libs.datastore.preferences)
    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.hilt.android)
    kapt(libs.hilt.android.compiler)

    // Firebase Cloud Messaging. The google-services Gradle plugin is intentionally
    // NOT applied (it would require a checked-in google-services.json at build
    // time); Firebase is initialized at runtime, gated on config presence.
    implementation(platform(libs.firebase.bom))
    implementation(libs.firebase.messaging)

    testImplementation(libs.junit)
    testImplementation(libs.kotlinx.coroutines.test)
    testImplementation(libs.kotlinx.serialization.json)
    testImplementation(libs.okhttp.mockwebserver)
    testImplementation(libs.okhttp)
}
