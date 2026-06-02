package dev.forge.feature.chat.ui

import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.sp

/** Rename a session — prefilled with the current title; Save commits the trimmed text. */
@Composable
fun RenameSessionDialog(
    current: String?,
    onConfirm: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    var text by remember { mutableStateOf(current.orEmpty()) }
    AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = SurfaceContainerHigh,
        title = { Text("Rename session", color = OnSurface) },
        text = {
            OutlinedTextField(
                value = text,
                onValueChange = { text = it },
                singleLine = true,
                label = { Text("Title") },
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                keyboardActions = KeyboardActions(onDone = {
                    if (text.isNotBlank()) onConfirm(text)
                }),
            )
        },
        confirmButton = {
            TextButton(
                onClick = { onConfirm(text) },
                enabled = text.isNotBlank(),
            ) { Text("Save") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

/**
 * Share controls. With no link yet: offer to create one. With a link: show it, copy, or revoke.
 * The link is read live from the session, so it appears once [onShare] round-trips.
 */
@Composable
fun ShareSessionDialog(
    url: String?,
    onShare: () -> Unit,
    onUnshare: () -> Unit,
    onCopy: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = SurfaceContainerHigh,
        title = { Text("Share session", color = OnSurface) },
        text = {
            if (url == null) {
                Text(
                    "This session isn't shared. Create a public link to it?",
                    color = OnSurfaceVariant,
                )
            } else {
                Text(url, fontFamily = ForgeMono, fontSize = 12.5.sp, color = OnSurface)
            }
        },
        confirmButton = {
            if (url == null) {
                TextButton(onClick = onShare) { Text("Create link") }
            } else {
                TextButton(onClick = { onCopy(url) }) { Text("Copy link") }
            }
        },
        dismissButton = {
            if (url == null) {
                TextButton(onClick = onDismiss) { Text("Cancel") }
            } else {
                TextButton(onClick = onUnshare) { Text("Unshare") }
            }
        },
    )
}
