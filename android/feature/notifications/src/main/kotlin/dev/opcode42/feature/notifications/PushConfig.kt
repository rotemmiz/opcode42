package dev.opcode42.feature.notifications

import android.content.Context
import android.util.Log
import com.google.firebase.FirebaseApp
import com.google.firebase.FirebaseOptions

/**
 * Runtime gate for Firebase Cloud Messaging.
 *
 * Live FCM needs a real Firebase project. Rather than apply the
 * `com.google.gms:google-services` Gradle plugin (which fails the build without a
 * checked-in `google-services.json` — a file we deliberately do NOT commit), we
 * read the Firebase identifiers from optional string resources and initialize
 * Firebase manually, ONLY when they are present and non-blank.
 *
 * Wire these by dropping a `google-services.json` into `:app` and running the
 * gms plugin's `processDebugGoogleServices` to generate the resources, OR by
 * adding a private `firebase_config.xml` defining:
 *
 *   firebase_project_id, firebase_application_id, firebase_api_key, firebase_messaging_sender_id
 *
 * When any required value is absent the app builds and runs normally with push
 * disabled (the no-google-services CI path).
 */
object PushConfig {
    private const val TAG = "PushConfig"

    private fun res(context: Context, name: String): String? {
        val id = context.resources.getIdentifier(name, "string", context.packageName)
        if (id == 0) return null
        return context.getString(id).takeIf { it.isNotBlank() }
    }

    /**
     * Reports whether FCM is configured for this build. False on the
     * no-google-services path; callers must skip all FCM work when false.
     */
    fun isConfigured(context: Context): Boolean = options(context) != null

    private fun options(context: Context): FirebaseOptions? {
        val appId = res(context, "firebase_application_id") ?: return null
        val apiKey = res(context, "firebase_api_key") ?: return null
        val projectId = res(context, "firebase_project_id") ?: return null
        val builder = FirebaseOptions.Builder()
            .setApplicationId(appId)
            .setApiKey(apiKey)
            .setProjectId(projectId)
        res(context, "firebase_messaging_sender_id")?.let { builder.setGcmSenderId(it) }
        return builder.build()
    }

    /**
     * Idempotently initializes the default FirebaseApp from the resource-backed
     * options. Returns true when Firebase is (now) available, false when push is
     * not configured. Safe to call repeatedly.
     */
    fun ensureInitialized(context: Context): Boolean {
        val existing = runCatching { FirebaseApp.getInstance() }.getOrNull()
        if (existing != null) return true
        val options = options(context) ?: return false
        return try {
            FirebaseApp.initializeApp(context.applicationContext, options)
            true
        } catch (t: Throwable) {
            Log.w(TAG, "Firebase init failed; push disabled: ${t.message}")
            false
        }
    }
}
