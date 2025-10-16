package main

import (
	"io"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"

)

func newEncryptionKey() []byte {
	key := make([]byte, 32)

	io.ReadFull(rand.Reader, key)
	return key

}

func copyDecrypt(key []byte, src io.Reader, dst io.Writer) (int, error) {
	block , err := aes.NewCipher(key)
	if err != nil {
		return 0, err
	}

	//read the iv from the given io.reader which should be the first 16 bytes of it
	iv := make([]byte, block.BlockSize())
	if _, err := src.Read(iv); err != nil {
		return 0, err
	}

	var (buf = make([]byte, 32 * 1024) // about 3kb read limit

		stream = cipher.NewCTR(block, iv)
		nw  = block.BlockSize()
	)

	for {
		n, err := src.Read(buf)
		if n > 0 {
			stream.XORKeyStream(buf, buf[:n])
			nn , err := dst.Write(buf[:n])
			if err != nil {
				return 0, err
			}
			nw += nn

		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
	}
	return nw, nil
}



func copyEncrypt(key []byte, src io.Reader, dst io.Writer) (int, error) {
	block , err := aes.NewCipher(key)
	if err != nil {
		return 0, err
	}
	iv := make([]byte, block.BlockSize()) // 16 bytes, we need to keep this in the file
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return 0, err
	}
	 //prepend the iv to the file to decode it on the other side
	if _, err := dst.Write(iv); err != nil {
		return 0, err
	}

	var (buf = make([]byte, 32 * 1024)

	stream = cipher.NewCTR(block, iv)
	nw  = block.BlockSize()
	)

	for {
		n , err := src.Read(buf)

		if n > 0{
			stream.XORKeyStream(buf, buf[:n])
			nn , err := dst.Write(buf[:n])
			if err!= nil {
				return 0, err
			}
			nw += nn
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}

	}

	return nw, nil
}