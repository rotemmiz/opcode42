package dev.forge.feature.chat.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Send
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * Sticky bottom prompt input.
 * Design: surfaceContainer bg, 2dp primary left border, 48dp min height,
 * paper-plane send button in primary color. (design/android/README.md §5)
 */
@Composable
fun PromptInput(
    onSend: (String) -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    var text by remember { mutableStateOf("") }

    Row(
        verticalAlignment = Alignment.Bottom,
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = 12.dp, vertical = 8.dp),
    ) {
        // Text field with 2dp primary left accent bar
        Box(
            modifier = Modifier
                .weight(1f)
                .heightIn(min = 48.dp)
                .clip(RoundedCornerShape(6.dp))
                .background(SurfaceContainer)
                .border(1.dp, Hairline, RoundedCornerShape(6.dp))
                // 2dp primary left bar via inner box
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

        // Send button — 40dp blue square
        IconButton(
            onClick = {
                val trimmed = text.trim()
                if (trimmed.isNotEmpty() && enabled) {
                    onSend(trimmed)
                    text = ""
                }
            },
            modifier = Modifier
                .size(40.dp)
                .clip(RoundedCornerShape(6.dp))
                .background(if (text.isNotBlank() && enabled) Primary else Hairline),
        ) {
            Icon(
                Icons.Default.Send,
                contentDescription = "Send",
                tint = if (text.isNotBlank() && enabled) OnPrimary else OnSurfaceFaint,
                modifier = Modifier.size(20.dp),
            )
        }
    }
}
