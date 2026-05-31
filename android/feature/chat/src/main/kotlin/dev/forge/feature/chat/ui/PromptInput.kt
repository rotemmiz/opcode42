package dev.forge.feature.chat.ui

import android.util.Base64
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.AttachFile
import androidx.compose.material.icons.filled.Close
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.forge.core.model.FilePartInput

/**
 * Sticky bottom prompt input.
 * Design: surfaceContainer bg, 2dp primary left border, 48dp min height,
 * paper-plane send button in primary color. (design/android/README.md §5)
 */
@Composable
fun PromptInput(
    onSend: (text: String, attachments: List<FilePartInput>) -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    var text by remember { mutableStateOf("") }
    var attachments by remember { mutableStateOf<List<FilePartInput>>(emptyList()) }
    var attachmentNames by remember { mutableStateOf<List<String>>(emptyList()) }

    val context = LocalContext.current

    val filePicker = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        val mime = context.contentResolver.getType(uri) ?: "application/octet-stream"
        val bytes = context.contentResolver.openInputStream(uri)?.use { it.readBytes() }
            ?: return@rememberLauncherForActivityResult
        val b64 = Base64.encodeToString(bytes, Base64.NO_WRAP)
        val dataUrl = "data:$mime;base64,$b64"
        val name = uri.lastPathSegment ?: "file"
        attachments = attachments + FilePartInput(mime = mime, url = dataUrl)
        attachmentNames = attachmentNames + name
    }

    Column(
        modifier = modifier
            .fillMaxWidth()
            .imePadding()
            .padding(horizontal = 12.dp, vertical = 8.dp),
    ) {
        if (attachments.isNotEmpty()) {
            LazyRow(
                horizontalArrangement = Arrangement.spacedBy(6.dp),
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(bottom = 6.dp),
            ) {
                items(attachments.indices.toList()) { idx ->
                    AttachmentChip(
                        name = attachmentNames[idx],
                        onRemove = {
                            attachments = attachments.toMutableList().also { it.removeAt(idx) }
                            attachmentNames = attachmentNames.toMutableList().also { it.removeAt(idx) }
                        },
                    )
                }
            }
        }

        Row(verticalAlignment = Alignment.Bottom) {
            // Text field with 2dp primary left accent bar
            Box(
                modifier = Modifier
                    .weight(1f)
                    .heightIn(min = 48.dp)
                    .clip(RoundedCornerShape(6.dp))
                    .background(SurfaceContainer)
                    .border(1.dp, Hairline, RoundedCornerShape(6.dp)),
            ) {
                Row(Modifier.fillMaxWidth()) {
                    Box(
                        modifier = Modifier
                            .width(2.dp)
                            .heightIn(min = 48.dp)
                            .background(Primary),
                    )
                    BasicTextField(
                        value = text,
                        onValueChange = { text = it },
                        textStyle = TextStyle(
                            color = OnSurface,
                            fontFamily = FontFamily.Monospace,
                            fontSize = 13.5.sp,
                        ),
                        cursorBrush = SolidColor(Primary),
                        modifier = Modifier
                            .weight(1f)
                            .padding(horizontal = 10.dp, vertical = 12.dp),
                        decorationBox = { inner ->
                            if (text.isEmpty()) {
                                Text(
                                    "Ask anything…  /  @",
                                    color = OnSurfaceGhost,
                                    fontFamily = FontFamily.Monospace,
                                    fontSize = 13.5.sp,
                                )
                            }
                            inner()
                        },
                    )
                }
            }

            Spacer(Modifier.width(8.dp))

            // Paperclip button — 48dp touch target
            IconButton(
                onClick = { filePicker.launch("*/*") },
                enabled = enabled,
                modifier = Modifier
                    .size(48.dp)
                    .clip(RoundedCornerShape(6.dp))
                    .background(SurfaceContainer),
            ) {
                Icon(
                    Icons.Default.AttachFile,
                    contentDescription = "Attach file",
                    tint = if (enabled) OnSurfaceVariant else OnSurfaceFaint,
                    modifier = Modifier.size(20.dp),
                )
            }

            Spacer(Modifier.width(8.dp))

            // Send button — 48dp blue square (M3 minimum touch target)
            val canSend = enabled && (text.isNotBlank() || attachments.isNotEmpty())
            IconButton(
                onClick = {
                    val trimmed = text.trim()
                    if (trimmed.isNotEmpty() || attachments.isNotEmpty()) {
                        onSend(trimmed, attachments)
                        text = ""
                        attachments = emptyList()
                        attachmentNames = emptyList()
                    }
                },
                enabled = canSend,
                modifier = Modifier
                    .size(48.dp)
                    .clip(RoundedCornerShape(6.dp))
                    .background(if (canSend) Primary else Hairline),
            ) {
                Icon(
                    Icons.AutoMirrored.Filled.Send,
                    contentDescription = "Send",
                    tint = if (canSend) OnPrimary else OnSurfaceFaint,
                    modifier = Modifier.size(20.dp),
                )
            }
        }
    }
}

@Composable
private fun AttachmentChip(name: String, onRemove: () -> Unit) {
    Surface(
        shape = RoundedCornerShape(16.dp),
        color = SurfaceContainer,
        border = BorderStroke(1.dp, Hairline),
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier.padding(start = 10.dp, end = 4.dp, top = 4.dp, bottom = 4.dp),
        ) {
            Text(
                text = name,
                color = OnSurface,
                fontFamily = FontFamily.Monospace,
                fontSize = 12.sp,
                maxLines = 1,
            )
            // 32dp button keeps overall chip height reasonable while meeting minimum touch target
            IconButton(
                onClick = onRemove,
                modifier = Modifier.size(32.dp),
            ) {
                Icon(
                    Icons.Default.Close,
                    contentDescription = "Remove attachment",
                    tint = OnSurfaceVariant,
                    modifier = Modifier.size(14.dp),
                )
            }
        }
    }
}
